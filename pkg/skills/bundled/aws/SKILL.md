---
name: aws
description: "AWS CLI operations — S3, EC2, Lambda, IAM, CloudFormation, and more"
require_bins: ["aws"]
---

# AWS CLI

## When to Use
- Managing AWS resources from the command line
- S3 file operations
- EC2 instance management
- Lambda function management
- IAM user and role management

## Configuration
```
aws configure                           # Set up credentials interactively
aws sts get-caller-identity             # Verify current identity
aws configure list-profiles             # List configured profiles
aws --profile <name> ...                # Use specific profile
```

## S3
```
aws s3 ls                               # List buckets
aws s3 ls s3://bucket/prefix/           # List objects
aws s3 cp file.txt s3://bucket/         # Upload file
aws s3 cp s3://bucket/file.txt .        # Download file
aws s3 sync ./dir s3://bucket/dir       # Sync directory
aws s3 rm s3://bucket/file.txt          # Delete object
aws s3 mb s3://new-bucket               # Create bucket
```

## EC2
```
aws ec2 describe-instances              # List instances
aws ec2 describe-instances --filters "Name=instance-state-name,Values=running"
aws ec2 start-instances --instance-ids i-xxx
aws ec2 stop-instances --instance-ids i-xxx
aws ec2 describe-security-groups        # List security groups
```

## Lambda
```
aws lambda list-functions               # List functions
aws lambda invoke --function-name <name> output.json  # Invoke function
aws lambda get-function --function-name <name>        # Get function config
aws lambda update-function-code --function-name <name> --zip-file fileb://code.zip
```

## IAM
```
aws iam list-users                      # List IAM users
aws iam list-roles                      # List roles
aws iam get-user                        # Current user info
```

## CloudFormation
```
aws cloudformation list-stacks          # List stacks
aws cloudformation describe-stacks --stack-name <name>
aws cloudformation deploy --template-file template.yaml --stack-name <name>
```

## Tips
- Use `--output table` for human-readable output, `--output json` for scripts
- Use `--query` (JMESPath) to filter results: `aws ec2 describe-instances --query 'Reservations[].Instances[].InstanceId'`
- Use `--region` to target a specific region
- Use `--dry-run` on EC2 commands to test permissions without executing
