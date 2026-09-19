#!/bin/bash
# Deployment environment variables for Night of a Thousand Pixels

# AWS Configuration
export AWS_REGION="us-east-1"
export AWS_ACCOUNT_ID="${AWS_ACCOUNT_ID:?set AWS_ACCOUNT_ID before sourcing this file}"

# ECR Configuration
export ECR_REPOSITORY_NAME="nightofpixels"
export ECR_URI="${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/${ECR_REPOSITORY_NAME}"

# ECS Configuration
export ECS_CLUSTER_NAME="nightofpixels"
export ECS_SERVICE_NAME="nightofpixels-service"
export ECS_TASK_FAMILY="nightofpixels"

# Database Configuration (UPDATE THESE!)
export DB_ENDPOINT="your-rds-endpoint.us-east-1.rds.amazonaws.com"
export DB_PASSWORD="CHANGE_THIS_PASSWORD"
export DB_NAME="nightofpixels"
export DB_USER="postgres"
export DATABASE_URL="postgresql://${DB_USER}:${DB_PASSWORD}@${DB_ENDPOINT}:5432/${DB_NAME}"

# Application Configuration
export COOKIE_SECRET="$(openssl rand -hex 32)"
export ORIGIN="https://pixels.eraith.dev"
export NEXT_PUBLIC_SITE_NAME="Night of a Thousand Pixels"

# Secrets Manager ARNs (will be created)
export SECRET_DB_ARN="arn:aws:secretsmanager:${AWS_REGION}:${AWS_ACCOUNT_ID}:secret:nightofpixels/database_url"
export SECRET_COOKIE_ARN="arn:aws:secretsmanager:${AWS_REGION}:${AWS_ACCOUNT_ID}:secret:nightofpixels/cookie_secret"

# IAM Role ARN
export TASK_EXECUTION_ROLE_ARN="arn:aws:iam::${AWS_ACCOUNT_ID}:role/ecsTaskExecutionRole"

# Load Balancer Configuration
export ALB_NAME="nightofpixels-alb"
export TARGET_GROUP_NAME="nightofpixels-tg"

echo "✓ Environment variables loaded!"
echo "ECR URI: ${ECR_URI}"
echo "Database URL: postgresql://${DB_USER}:***@${DB_ENDPOINT}:5432/${DB_NAME}"
echo "Task Execution Role: ${TASK_EXECUTION_ROLE_ARN}"
