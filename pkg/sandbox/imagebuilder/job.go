package imagebuilder

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Build creates a ConfigMap with the generated Dockerfile and spawns a Kaniko
// Job to build and push the image. Returns immediately after the Job is created.
// Use WaitForBuild or StreamLogs to track progress.
func (b *Builder) Build(ctx context.Context, spec BuildSpec, onProgress ProgressFunc) (*BuildResult, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	combined := CombinedBody(spec.PlatformBody, spec.TeamBody)
	if strings.TrimSpace(spec.PlatformBody) == "" && strings.TrimSpace(spec.TeamBody) == "" {
		return nil, fmt.Errorf("imagebuilder: at least one of platform or team dockerfile body is required")
	}
	if spec.BaseImage == "" {
		return nil, fmt.Errorf("imagebuilder: base image is required")
	}

	destImage := b.ImageTag(spec)
	jobName := JobName(spec.Scope, combined)
	cmName := ConfigMapName(spec.Scope, combined)

	// Generate full Dockerfile (FROM + platform body + team body).
	dockerfile := GenerateDockerfile(spec.BaseImage, spec.PlatformBody, spec.TeamBody)

	if onProgress != nil {
		onProgress(ctx, "Generating Dockerfile...")
	}

	// Delete any pre-existing ConfigMap/Job from a previous build attempt.
	_ = b.cfg.Client.CoreV1().ConfigMaps(b.cfg.Namespace).Delete(ctx, cmName, metav1.DeleteOptions{})
	_ = b.cfg.Client.BatchV1().Jobs(b.cfg.Namespace).Delete(ctx, jobName, *deleteOpts())

	// Brief pause to allow K8s to process deletions.
	time.Sleep(500 * time.Millisecond)

	// Create ConfigMap with Dockerfile.
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: b.cfg.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/component":  "image-builder",
				"app.kubernetes.io/managed-by": "astonish",
				"astonish.io/build-scope":      sanitizeDNS(spec.Scope),
			},
		},
		Data: map[string]string{
			"Dockerfile": dockerfile,
		},
	}

	if onProgress != nil {
		onProgress(ctx, "Creating build context...")
	}

	if _, err := b.cfg.Client.CoreV1().ConfigMaps(b.cfg.Namespace).Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("imagebuilder: create configmap: %w", err)
	}

	// Build Kaniko Job.
	backoffLimit := int32(0)
	ttl := int32(300) // 5 min TTL after completion
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: b.cfg.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/component":  "image-builder",
				"app.kubernetes.io/managed-by": "astonish",
				"astonish.io/build-scope":      sanitizeDNS(spec.Scope),
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/component":  "image-builder",
						"app.kubernetes.io/managed-by": "astonish",
						"astonish.io/build-scope":      sanitizeDNS(spec.Scope),
					},
					Annotations: map[string]string{
						// Skip Istio sidecar — Kaniko doesn't need mesh,
						// and the sidecar can interfere with Job completion.
						"sidecar.istio.io/inject": "false",
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:  "kaniko",
						Image: b.cfg.BuildImage,
						Args: []string{
							"--dockerfile=/workspace/Dockerfile",
							"--context=dir:///workspace",
							fmt.Sprintf("--destination=%s", destImage),
							"--cache=true",
							fmt.Sprintf("--cache-repo=%s/astonish-cache", b.cfg.RegistryURL),
							"--snapshotMode=redo",
							"--compressed-caching=false",
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "dockerfile", MountPath: "/workspace", ReadOnly: true},
							{Name: "docker-config", MountPath: "/kaniko/.docker", ReadOnly: true},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("512Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("2"),
								corev1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
					}},
					Volumes: []corev1.Volume{
						{
							Name: "dockerfile",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: cmName},
								},
							},
						},
						{
							Name: "docker-config",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: b.cfg.SecretName,
									Items: []corev1.KeyToPath{
										{Key: ".dockerconfigjson", Path: "config.json"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if onProgress != nil {
		onProgress(ctx, fmt.Sprintf("Starting build job (%s)...", jobName))
	}

	if _, err := b.cfg.Client.BatchV1().Jobs(b.cfg.Namespace).Create(ctx, job, metav1.CreateOptions{}); err != nil {
		// Cleanup ConfigMap on failure.
		_ = b.cfg.Client.CoreV1().ConfigMaps(b.cfg.Namespace).Delete(ctx, cmName, metav1.DeleteOptions{})
		return nil, fmt.Errorf("imagebuilder: create job: %w", err)
	}

	slog.Info("image build job created",
		"job", jobName,
		"namespace", b.cfg.Namespace,
		"destination", destImage,
		"scope", spec.Scope,
	)

	return &BuildResult{
		Image:         destImage,
		JobName:       jobName,
		ConfigMapName: cmName,
	}, nil
}

// Cleanup removes the ConfigMap and Job resources for a completed build.
func (b *Builder) Cleanup(ctx context.Context, result *BuildResult) {
	if result == nil {
		return
	}
	if result.ConfigMapName != "" {
		_ = b.cfg.Client.CoreV1().ConfigMaps(b.cfg.Namespace).Delete(ctx, result.ConfigMapName, metav1.DeleteOptions{})
	}
	if result.JobName != "" {
		_ = b.cfg.Client.BatchV1().Jobs(b.cfg.Namespace).Delete(ctx, result.JobName, *deleteOpts())
	}
}

func deleteOpts() *metav1.DeleteOptions {
	propagation := metav1.DeletePropagationBackground
	return &metav1.DeleteOptions{PropagationPolicy: &propagation}
}
