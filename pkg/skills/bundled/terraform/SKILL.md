---
name: terraform
description: "Infrastructure as code with Terraform — plan, apply, state management"
require_bins: ["terraform"]
---

# Terraform

## When to Use
- Provisioning and managing cloud infrastructure
- Planning and applying infrastructure changes
- Managing Terraform state
- Working with modules

## Core Workflow
```
terraform init                          # Initialize (download providers/modules)
terraform plan                          # Preview changes
terraform plan -out=plan.tfplan         # Save plan to file
terraform apply                         # Apply changes (prompts for confirmation)
terraform apply plan.tfplan             # Apply saved plan (no prompt)
terraform apply -auto-approve           # Apply without prompt (use with caution)
terraform destroy                       # Destroy all managed resources
```

## State Management
```
terraform state list                    # List resources in state
terraform state show <resource>         # Show resource details
terraform state mv <src> <dst>          # Rename/move resource in state
terraform state rm <resource>           # Remove resource from state (won't destroy)
terraform state pull                    # Download remote state
terraform import <resource> <id>        # Import existing resource into state
```

## Workspaces
```
terraform workspace list                # List workspaces
terraform workspace new <name>          # Create workspace
terraform workspace select <name>       # Switch workspace
```

## Common Patterns
```
terraform fmt                           # Format .tf files
terraform validate                      # Validate configuration
terraform output                        # Show outputs
terraform output -json                  # Machine-readable outputs
terraform graph | dot -Tpng > graph.png # Visualize dependency graph
terraform providers                     # List required providers
```

## Tips
- Always run `terraform plan` before `terraform apply`
- Use `-target=<resource>` to apply changes to specific resources only
- Use `terraform fmt -recursive` to format all files in subdirectories
- Check `.terraform.lock.hcl` into version control for reproducible builds
- Use `TF_LOG=DEBUG` environment variable for verbose logging
