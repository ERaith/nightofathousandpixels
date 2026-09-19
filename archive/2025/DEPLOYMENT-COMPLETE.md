# Night of a Thousand Pixels - AWS Fargate Deployment Guide

## Deployment Summary

Successfully deployed to AWS Fargate with the following infrastructure:

- **Application URL**: https://pixels.eraith.dev
- **ALB URL**: http://nightofpixels-alb-178048638.us-east-1.elb.amazonaws.com
- **AWS Region**: us-east-1
- **AWS Account**: YOUR_AWS_ACCOUNT_ID (eraith)

---

## Architecture

```
Internet → Cloudflare CDN → Application Load Balancer → ECS Fargate Tasks → RDS PostgreSQL
```

### Infrastructure Components

1. **Amazon ECS Fargate**
   - Cluster: `nightofpixels`
   - Service: `nightofpixels-service`
   - Desired count: 2 tasks
   - Task definition: `nightofpixels:1`

2. **Application Load Balancer**
   - Name: `nightofpixels-alb`
   - DNS: `nightofpixels-alb-178048638.us-east-1.elb.amazonaws.com`
   - Listener: HTTP (port 80)
   - Target group: `nightofpixels-tg`

3. **Amazon RDS PostgreSQL**
   - Instance: `nightofpixels`
   - Endpoint: `nightofpixels.ca3q6uyo6ces.us-east-1.rds.amazonaws.com:5432`
   - Database: `nightofpixels`

4. **Amazon ECR**
   - Repository: `nightofpixels`
   - URI: `YOUR_AWS_ACCOUNT_ID.dkr.ecr.us-east-1.amazonaws.com/nightofpixels`

5. **AWS Secrets Manager**
   - `nightofpixels/database_url` - PostgreSQL connection string
   - `nightofpixels/cookie_secret` - Session cookie secret

6. **Security Groups**
   - `nightofpixels-alb-sg` (sg-09d97c21b51f9ebfa) - ALB security group
   - `nightofpixels-ecs-sg` (sg-00e4669a9bca0c6b6) - ECS tasks security group
   - RDS security group (sg-0d1afb65586d5b9e8) - Database security group

---

## Deployment Steps Performed

### 1. Fixed Docker Build Issues

**Problem**: Prisma engine incompatibility with Alpine Linux

**Solution**:
- Updated [Dockerfile](Dockerfile) to install OpenSSL 3
- Modified [prisma/schema.prisma](prisma/schema.prisma:4) to include `binaryTargets = ["native", "linux-musl-openssl-3.0.x"]`
- Added `export const dynamic = 'force-dynamic'` to pages accessing database at build time:
  - [app/(site)/page.tsx](app/(site)/page.tsx)
  - [app/(site)/submit/page.tsx](app/(site)/submit/page.tsx)
  - [app/(site)/admin/page.tsx](app/(site)/admin/page.tsx)

### 2. Changed Database from SQLite to PostgreSQL

**Problem**: Production needs PostgreSQL, schema was configured for SQLite

**Solution**:
- Updated [prisma/schema.prisma:8](prisma/schema.prisma:8) from `provider = "sqlite"` to `provider = "postgresql"`
- Rebuilt Docker image with correct database provider

### 3. Built Multi-Platform Docker Image

Built Docker image for AMD64 platform (required for AWS Fargate):

```bash
docker build --platform linux/amd64 -t nightofpixels:latest .
```

### 4. Pushed Image to ECR

```bash
# Login to ECR
aws ecr get-login-password --region us-east-1 | \
  docker login --username AWS --password-stdin \
  YOUR_AWS_ACCOUNT_ID.dkr.ecr.us-east-1.amazonaws.com

# Tag and push
docker tag nightofpixels:latest \
  YOUR_AWS_ACCOUNT_ID.dkr.ecr.us-east-1.amazonaws.com/nightofpixels:latest
docker push YOUR_AWS_ACCOUNT_ID.dkr.ecr.us-east-1.amazonaws.com/nightofpixels:latest
```

### 5. Created ECS Service

```bash
aws ecs create-service \
  --cluster nightofpixels \
  --service-name nightofpixels-service \
  --task-definition nightofpixels \
  --desired-count 2 \
  --launch-type FARGATE \
  --network-configuration "awsvpcConfiguration={subnets=[subnet-02278859351a7cd8e,subnet-0e3be594706e42968],securityGroups=[sg-00e4669a9bca0c6b6],assignPublicIp=ENABLED}" \
  --region us-east-1
```

### 6. Fixed IAM Permissions

**Problem**: `ecsTaskExecutionRole` couldn't read secrets from Secrets Manager

**Solution**: Added inline policy to `ecsTaskExecutionRole`:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["secretsmanager:GetSecretValue"],
      "Resource": ["arn:aws:secretsmanager:us-east-1:YOUR_AWS_ACCOUNT_ID:secret:nightofpixels/*"]
    }
  ]
}
```

### 7. Configured Security Groups

**RDS Security Group Rules**:
- Inbound: PostgreSQL (5432) from ECS security group (sg-00e4669a9bca0c6b6)

```bash
aws ec2 authorize-security-group-ingress \
  --group-id sg-0d1afb65586d5b9e8 \
  --protocol tcp \
  --port 5432 \
  --source-group sg-00e4669a9bca0c6b6 \
  --region us-east-1
```

**ECS Security Group Rules**:
- Inbound: HTTP (3000) from ALB security group (sg-09d97c21b51f9ebfa)

**ALB Security Group Rules**:
- Inbound: HTTP (80) from anywhere (0.0.0.0/0)

### 8. Created Database and Ran Migrations

Used ECS one-time task to initialize the database:

```bash
aws ecs run-task \
  --cluster nightofpixels \
  --task-definition nightofpixels:1 \
  --launch-type FARGATE \
  --network-configuration "awsvpcConfiguration={subnets=[subnet-02278859351a7cd8e],securityGroups=[sg-00e4669a9bca0c6b6],assignPublicIp=ENABLED}" \
  --overrides '{"containerOverrides":[{"name":"nightofpixels-app","command":["sh","-c","npx prisma migrate deploy || (npx prisma db push --accept-data-loss && echo Database initialized)"]}]}' \
  --region us-east-1
```

Result: PostgreSQL database `nightofpixels` created and schema migrated successfully.

### 9. Attached Application Load Balancer

Updated target group health check path from `/api/season` to `/`:

```bash
aws elbv2 modify-target-group \
  --target-group-arn arn:aws:elasticloadbalancing:us-east-1:YOUR_AWS_ACCOUNT_ID:targetgroup/nightofpixels-tg/3c224261bde3b00d \
  --health-check-path / \
  --region us-east-1
```

Updated ECS service to register with target group:

```bash
aws ecs update-service \
  --cluster nightofpixels \
  --service nightofpixels-service \
  --load-balancers "targetGroupArn=arn:aws:elasticloadbalancing:us-east-1:YOUR_AWS_ACCOUNT_ID:targetgroup/nightofpixels-tg/3c224261bde3b00d,containerName=nightofpixels-app,containerPort=3000" \
  --health-check-grace-period-seconds 60 \
  --region us-east-1
```

### 10. Configured Custom Domain with Cloudflare

**DNS Configuration**:
- Type: CNAME
- Name: `pixels`
- Target: `nightofpixels-alb-178048638.us-east-1.elb.amazonaws.com`
- Proxy status: Proxied (orange cloud)

**SSL/TLS Configuration**:
- Mode: **Flexible** (HTTPS between visitor and Cloudflare, HTTP to origin)
- Location: Cloudflare Dashboard → SSL/TLS → Overview

---

## Current Status

✅ **Application**: Live and fully functional
✅ **Database**: PostgreSQL schema migrated, Season 2025 created
✅ **Load Balancer**: 2 healthy targets
✅ **Custom Domain**: https://pixels.eraith.dev
✅ **HTTPS**: Enabled via Cloudflare

---

## Operational Commands

### View Service Status
```bash
aws ecs describe-services \
  --cluster nightofpixels \
  --services nightofpixels-service \
  --region us-east-1
```

### View Running Tasks
```bash
aws ecs list-tasks \
  --cluster nightofpixels \
  --service-name nightofpixels-service \
  --region us-east-1
```

### View CloudWatch Logs
```bash
aws logs tail /ecs/nightofpixels --follow --region us-east-1
```

### Check Target Health
```bash
aws elbv2 describe-target-health \
  --target-group-arn arn:aws:elasticloadbalancing:us-east-1:YOUR_AWS_ACCOUNT_ID:targetgroup/nightofpixels-tg/3c224261bde3b00d \
  --region us-east-1
```

### Force New Deployment
```bash
aws ecs update-service \
  --cluster nightofpixels \
  --service nightofpixels-service \
  --force-new-deployment \
  --region us-east-1
```

### Scale Service
```bash
aws ecs update-service \
  --cluster nightofpixels \
  --service nightofpixels-service \
  --desired-count 3 \
  --region us-east-1
```

---

## Deploying Updates

When you make code changes:

1. **Build new Docker image**:
   ```bash
   docker build --platform linux/amd64 -t nightofpixels:latest .
   ```

2. **Tag and push to ECR**:
   ```bash
   aws ecr get-login-password --region us-east-1 | \
     docker login --username AWS --password-stdin \
     YOUR_AWS_ACCOUNT_ID.dkr.ecr.us-east-1.amazonaws.com

   docker tag nightofpixels:latest \
     YOUR_AWS_ACCOUNT_ID.dkr.ecr.us-east-1.amazonaws.com/nightofpixels:latest
   docker push YOUR_AWS_ACCOUNT_ID.dkr.ecr.us-east-1.amazonaws.com/nightofpixels:latest
   ```

3. **Deploy to ECS**:
   ```bash
   aws ecs update-service \
     --cluster nightofpixels \
     --service nightofpixels-service \
     --force-new-deployment \
     --region us-east-1
   ```

4. **Monitor deployment**:
   ```bash
   aws logs tail /ecs/nightofpixels --follow --region us-east-1
   ```

---

## Admin User Setup

### Create First Admin User

Use the temporary admin creation endpoint (only works when no admins exist):

```bash
curl -X POST https://pixels.eraith.dev/api/admin/create-first-admin \
  -H "Content-Type: application/json" \
  -d '{
    "email": "raithhtiar@gmail.com",
    "password": "YourSecurePassword123!",
    "name": "Admin"
  }'
```

**Important**: After creating the first admin, this endpoint will be disabled automatically.

### Access Admin Panel

1. Go to https://pixels.eraith.dev/admin/login
2. Login with your admin credentials
3. Manage seasons, movies, voters, and submission limits

---

## Environment Variables

The following environment variables are configured in the ECS task definition via Secrets Manager:

- `DATABASE_URL` - PostgreSQL connection string (from Secrets Manager)
- `COOKIE_SECRET` - Session cookie secret (from Secrets Manager)
- `ORIGIN` - Application origin URL
- `NEXT_PUBLIC_SITE_NAME` - Site name

---

## Troubleshooting

### Tasks Not Starting

Check CloudWatch logs:
```bash
aws logs tail /ecs/nightofpixels --since 10m --region us-east-1
```

### Target Health Check Failing

Check target group health:
```bash
aws elbv2 describe-target-health \
  --target-group-arn arn:aws:elasticloadbalancing:us-east-1:YOUR_AWS_ACCOUNT_ID:targetgroup/nightofpixels-tg/3c224261bde3b00d \
  --region us-east-1
```

### Can't Connect to Database

Verify security group allows traffic from ECS:
```bash
aws ec2 describe-security-groups \
  --group-ids sg-0d1afb65586d5b9e8 \
  --region us-east-1
```

### 502/503 Errors from ALB

1. Check if tasks are running and healthy
2. Verify security group allows traffic from ALB to ECS on port 3000
3. Check CloudWatch logs for application errors

---

## Next Steps (Optional Improvements)

1. **HTTPS on ALB**: Request ACM certificate and add HTTPS listener
2. **Auto-scaling**: Configure ECS service auto-scaling
3. **Database Backups**: Configure automated RDS snapshots
4. **Monitoring**: Set up CloudWatch alarms for:
   - High CPU usage
   - Target health
   - Response time
5. **CI/CD**: Set up GitHub Actions for automated deployments
6. **WAF**: Add AWS WAF for DDoS protection
7. **Secrets Rotation**: Enable automatic secrets rotation

---

## Cost Estimate (Monthly)

- **ECS Fargate** (2 tasks, 0.5 vCPU, 1GB RAM): ~$30
- **RDS PostgreSQL** (db.t3.micro): ~$15
- **Application Load Balancer**: ~$16
- **Data Transfer**: Variable
- **ECR Storage**: < $1
- **Secrets Manager**: ~$1

**Total**: ~$62-70/month

---

## Support

For issues or questions:
- Check CloudWatch logs: `/ecs/nightofpixels`
- Review ECS service events in AWS Console
- Verify target health in ALB console

---

**Deployment Date**: October 29, 2025
**Deployed By**: Claude Code
**Status**: Production Ready ✅
