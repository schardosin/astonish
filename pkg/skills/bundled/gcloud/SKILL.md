---
name: gcloud
description: "Google Cloud CLI — compute, storage, GKE, Cloud Run, IAM"
require_bins: ["gcloud"]
---

# Google Cloud CLI (gcloud)

## When to Use
- Managing Google Cloud resources
- Compute Engine instances
- Google Kubernetes Engine (GKE)
- Cloud Storage operations
- Cloud Run deployments

## Configuration
```
gcloud auth login                       # Authenticate interactively
gcloud auth list                        # List authenticated accounts
gcloud config get-value project         # Show current project
gcloud config set project <project-id>  # Set project
gcloud config set compute/region <region>  # Set default region
gcloud projects list                    # List accessible projects
```

## Compute Engine
```
gcloud compute instances list           # List instances
gcloud compute instances create <name> --machine-type=e2-medium --zone=us-central1-a
gcloud compute instances start <name>   # Start instance
gcloud compute instances stop <name>    # Stop instance
gcloud compute ssh <name>               # SSH into instance
gcloud compute instances describe <name>  # Instance details
```

## Cloud Storage (gsutil)
```
gsutil ls                               # List buckets
gsutil ls gs://bucket/                  # List objects
gsutil cp file.txt gs://bucket/         # Upload
gsutil cp gs://bucket/file.txt .        # Download
gsutil rsync -r ./dir gs://bucket/dir   # Sync directory
gsutil mb gs://new-bucket               # Create bucket
```

## GKE
```
gcloud container clusters list          # List clusters
gcloud container clusters get-credentials <cluster> --zone <zone>  # Get kubeconfig
gcloud container clusters create <name> --num-nodes=3
```

## Cloud Run
```
gcloud run services list                # List services
gcloud run deploy <service> --image=<image> --region=<region>
gcloud run services describe <service>  # Service details
gcloud run services delete <service>    # Delete service
```

## IAM
```
gcloud iam service-accounts list        # List service accounts
gcloud projects get-iam-policy <project>  # View IAM bindings
```

## Tips
- Use `--format=json` for machine-readable output
- Use `--filter` for server-side filtering: `gcloud compute instances list --filter="status=RUNNING"`
- Use `gcloud components update` to update the CLI
- Use `gcloud beta` or `gcloud alpha` for preview features
