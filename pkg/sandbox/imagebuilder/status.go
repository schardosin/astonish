package imagebuilder

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetBuildStatus checks the current status of a build Job.
func (b *Builder) GetBuildStatus(ctx context.Context, jobName string) (*BuildStatus, error) {
	job, err := b.cfg.Client.BatchV1().Jobs(b.cfg.Namespace).Get(ctx, jobName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("imagebuilder: get job: %w", err)
	}

	return jobToStatus(job), nil
}

// WaitForBuild polls the Job status until it completes or the context is cancelled.
// It calls onProgress with log lines as they become available.
func (b *Builder) WaitForBuild(ctx context.Context, jobName string, onProgress ProgressFunc) (*BuildStatus, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	logStreamed := false

	for {
		select {
		case <-ctx.Done():
			return &BuildStatus{Phase: BuildPhaseUnknown, Message: "context cancelled"}, ctx.Err()
		case <-ticker.C:
			status, err := b.GetBuildStatus(ctx, jobName)
			if err != nil {
				return nil, err
			}

			switch status.Phase {
			case BuildPhaseSucceeded:
				// Try to stream final logs.
				if !logStreamed && onProgress != nil {
					b.streamPodLogs(ctx, jobName, onProgress)
				}
				return status, nil
			case BuildPhaseFailed:
				// Try to get error logs.
				if onProgress != nil {
					b.streamPodLogs(ctx, jobName, onProgress)
				}
				return status, nil
			case BuildPhaseRunning:
				// Stream logs if we haven't started yet.
				if !logStreamed && onProgress != nil {
					go func() {
						b.streamPodLogs(ctx, jobName, onProgress)
					}()
					logStreamed = true
				}
			}
		}
	}
}

// StreamLogs streams the build pod logs in real-time, calling onProgress for
// each line. Blocks until the pod completes or context is cancelled.
func (b *Builder) StreamLogs(ctx context.Context, jobName string, onProgress ProgressFunc) error {
	// Find the pod owned by this Job.
	podName, err := b.findJobPod(ctx, jobName)
	if err != nil {
		return err
	}
	if podName == "" {
		return fmt.Errorf("imagebuilder: no pod found for job %s", jobName)
	}

	follow := true
	req := b.cfg.Client.CoreV1().Pods(b.cfg.Namespace).GetLogs(podName, &corev1.PodLogOptions{
		Follow: follow,
	})

	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("imagebuilder: stream logs: %w", err)
	}
	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	// Increase scanner buffer for long log lines.
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if onProgress != nil {
			onProgress(ctx, line)
		}
	}

	return scanner.Err()
}

// streamPodLogs is a best-effort log reader (non-following).
func (b *Builder) streamPodLogs(ctx context.Context, jobName string, onProgress ProgressFunc) {
	podName, err := b.findJobPod(ctx, jobName)
	if err != nil || podName == "" {
		return
	}

	tailLines := int64(50)
	req := b.cfg.Client.CoreV1().Pods(b.cfg.Namespace).GetLogs(podName, &corev1.PodLogOptions{
		TailLines: &tailLines,
	})

	stream, err := req.Stream(ctx)
	if err != nil {
		return
	}
	defer stream.Close()

	data, err := io.ReadAll(io.LimitReader(stream, 64*1024))
	if err != nil {
		return
	}

	lines := splitLines(string(data))
	for _, line := range lines {
		if line != "" && onProgress != nil {
			onProgress(ctx, line)
		}
	}
}

// findJobPod returns the name of the pod owned by the given Job.
func (b *Builder) findJobPod(ctx context.Context, jobName string) (string, error) {
	pods, err := b.cfg.Client.CoreV1().Pods(b.cfg.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil {
		return "", fmt.Errorf("imagebuilder: list job pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return "", nil
	}
	return pods.Items[0].Name, nil
}

// jobToStatus converts a K8s Job to a BuildStatus.
func jobToStatus(job *batchv1.Job) *BuildStatus {
	for _, cond := range job.Status.Conditions {
		switch cond.Type {
		case batchv1.JobComplete:
			if cond.Status == corev1.ConditionTrue {
				return &BuildStatus{Phase: BuildPhaseSucceeded, Message: "Build completed successfully"}
			}
		case batchv1.JobFailed:
			if cond.Status == corev1.ConditionTrue {
				msg := cond.Message
				if msg == "" {
					msg = cond.Reason
				}
				if msg == "" {
					msg = "Build failed"
				}
				return &BuildStatus{Phase: BuildPhaseFailed, Message: msg}
			}
		}
	}

	if job.Status.Active > 0 {
		return &BuildStatus{Phase: BuildPhaseRunning, Message: "Build in progress"}
	}

	return &BuildStatus{Phase: BuildPhaseUnknown, Message: "Unknown state"}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
