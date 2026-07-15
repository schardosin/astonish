# Taps & Flow Store

Taps are git-based repositories of community and organization flows. They work like Homebrew taps — add a tap to gain access to its collection of pre-built flows that you can install, customize, and run.

## Adding a Tap

```bash
# Add a community tap (supports shorthand or full URL)
astonish tap add SAP/astonish-flows

# Add a private org tap (uses git credentials)
astonish tap add https://github.com/myorg/astonish-flows

# Add with a custom name
astonish tap add https://github.com/user/flows --name user-flows
```

## Managing Taps

```bash
# List installed taps
astonish tap list

# Update all tap manifests
astonish tap update

# Remove a tap
astonish tap remove devops
```

## Installing Flows from Taps

Use the flows store commands to browse and install from taps:

```bash
# List available flows from all taps
astonish flows store list

# Filter by tag
astonish flows store list --tag kubernetes

# Search for flows
astonish flows store search deploy

# Install a flow
astonish flows store install devops/deploy-k8s

# Uninstall
astonish flows store uninstall deploy-k8s

# Update all tap manifests
astonish flows store update
```

Installed flows appear in your Flows list in Studio and can be edited or run like any other flow.

## Tap Repository Structure

A tap repository follows a simple layout:

```
my-tap/
├── tap.yaml              # Tap metadata
├── flows/
│   ├── deploy-k8s.yaml
│   ├── pr-review.yaml
│   └── incident-response.yaml
└── README.md
```

The `tap.yaml` file declares the tap:

```yaml
name: devops
description: DevOps automation flows
author: Astonish Community
flows:
  - name: deploy-k8s
    description: Deploy a service to Kubernetes
    tags: [kubernetes, deploy]
  - name: pr-review
    description: Automated pull request review
    tags: [github, code-review]
```

## Publishing Your Flows

To share your flows as a tap:

1. Create a git repository with the structure above.
2. Add a `tap.yaml` with metadata for each flow.
3. Push to a git host (GitHub, GitLab, etc.).
4. Share the URL — anyone can `astonish tap add` it.

In cloud deployments, team flows can also be exported as a tap for use across environments.
