---
name: kubernetes
description: "Kubernetes cluster management via kubectl — pods, deployments, services, logs"
require_bins: ["kubectl"]
---

# Kubernetes (kubectl)

## When to Use
- Managing Kubernetes resources (pods, deployments, services)
- Viewing logs and debugging
- Scaling and updating deployments
- Port forwarding for local access

## Context & Namespace
```
kubectl config get-contexts             # List available contexts
kubectl config use-context <context>    # Switch context
kubectl config current-context          # Show current context
kubectl get namespaces                  # List namespaces
kubectl -n <namespace> ...              # Run command in specific namespace
```

## Viewing Resources
```
kubectl get pods                        # List pods
kubectl get pods -o wide                # Pods with extra info (node, IP)
kubectl get deployments                 # List deployments
kubectl get services                    # List services
kubectl get all                         # List all resources
kubectl describe pod <pod>              # Detailed pod info
kubectl get pod <pod> -o yaml           # Full YAML spec
```

## Logs & Debugging
```
kubectl logs <pod>                      # View pod logs
kubectl logs -f <pod>                   # Follow logs
kubectl logs <pod> -c <container>       # Specific container in multi-container pod
kubectl logs <pod> --previous           # Logs from previous crashed instance
kubectl exec -it <pod> -- bash          # Shell into pod
kubectl port-forward <pod> 8080:80      # Forward local port to pod
kubectl top pods                        # Resource usage (requires metrics-server)
```

## Managing Deployments
```
kubectl apply -f manifest.yaml          # Apply a manifest
kubectl delete -f manifest.yaml         # Delete resources from manifest
kubectl scale deployment <name> --replicas=3  # Scale deployment
kubectl rollout status deployment <name>      # Watch rollout progress
kubectl rollout undo deployment <name>        # Rollback to previous version
kubectl set image deployment/<name> <container>=<image>:<tag>  # Update image
```

## Tips
- Use `-o json` or `-o yaml` for machine-readable output
- Use `kubectl get events --sort-by=.lastTimestamp` to debug issues
- Use `kubectl explain <resource>` for schema documentation
- Alias `kubectl` to `k` for faster typing: `alias k=kubectl`
