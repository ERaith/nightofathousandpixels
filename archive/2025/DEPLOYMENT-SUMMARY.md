# Night of a Thousand Pixels - Deployment Summary

**Deployment Date**: October 29, 2025
**Status**: ✅ Production Ready
**Application URL**: https://pixels.eraith.dev

---

## 🎉 What Was Deployed

Your movie voting application "Night of a Thousand Pixels" is now live on AWS Fargate with:

- **2 running containers** behind a load balancer for high availability
- **PostgreSQL database** on AWS RDS
- **Custom domain** with HTTPS via Cloudflare
- **Admin panel** for managing seasons, movies, and voters
- **Secure secrets** managed by AWS Secrets Manager

---

## 🏗️ Infrastructure

### AWS Services Used
- **ECS Fargate**: Serverless container orchestration (2 tasks)
- **Application Load Balancer**: Traffic distribution and health checks
- **RDS PostgreSQL**: Production database
- **ECR**: Docker image registry
- **Secrets Manager**: Secure credential storage
- **CloudWatch Logs**: Application logging

### Key Resources
| Resource | Name/ID | Purpose |
|----------|---------|---------|
| ECS Cluster | `nightofpixels` | Container orchestration |
| ECS Service | `nightofpixels-service` | Manages 2 running tasks |
| RDS Instance | `nightofpixels` | PostgreSQL database |
| ALB | `nightofpixels-alb` | Load balancer |
| Target Group | `nightofpixels-tg` | Health checks & routing |
| ECR Repository | `nightofpixels` | Docker images |
| Region | `us-east-1` | AWS region |

---

## 🔧 Issues Fixed During Deployment

### 1. Docker Build Compatibility
**Problem**: Prisma couldn't find OpenSSL libraries in Alpine Linux
**Solution**: Added OpenSSL 3 to Dockerfile and configured Prisma for `linux-musl-openssl-3.0.x`

### 2. Database Provider Mismatch
**Problem**: Schema configured for SQLite but production uses PostgreSQL
**Solution**: Changed `provider = "postgresql"` in schema.prisma

### 3. Platform Architecture
**Problem**: Built image for ARM64 (Apple Silicon) but Fargate needs AMD64
**Solution**: Rebuilt with `--platform linux/amd64`

### 4. Secrets Access
**Problem**: ECS tasks couldn't read from Secrets Manager
**Solution**: Added IAM policy to `ecsTaskExecutionRole` for `secretsmanager:GetSecretValue`

### 5. Network Connectivity
**Problem**: ECS tasks couldn't reach RDS database
**Solution**: Added security group rule allowing PostgreSQL (5432) from ECS to RDS

### 6. Health Check Failures
**Problem**: ALB health checks failing with 404
**Solution**: Changed health check path from `/api/season` to `/`

### 7. Build-Time Database Access
**Problem**: Next.js trying to access database during Docker build
**Solution**: Added `export const dynamic = 'force-dynamic'` to pages

---

## 📝 Admin Account

**Login URL**: https://pixels.eraith.dev/admin/login
**Admin Email**: raithhtiar@gmail.com
**Password**: [You set this when you created the admin]

### Admin Capabilities
- Manage seasons (create, lock/unlock, set dates)
- Hide/unhide movie submissions
- Delete movies or votes
- Set per-user submission limits
- Manage voter whitelist
- View all voters and their activity

---

## 🚀 Deploying Updates

When you make code changes, follow these steps:

```bash
# 1. Build for AMD64 platform
docker build --platform linux/amd64 -t nightofpixels:latest .

# 2. Login to ECR
aws ecr get-login-password --region us-east-1 | \
  docker login --username AWS --password-stdin \
  YOUR_AWS_ACCOUNT_ID.dkr.ecr.us-east-1.amazonaws.com

# 3. Tag and push
docker tag nightofpixels:latest \
  YOUR_AWS_ACCOUNT_ID.dkr.ecr.us-east-1.amazonaws.com/nightofpixels:latest
docker push YOUR_AWS_ACCOUNT_ID.dkr.ecr.us-east-1.amazonaws.com/nightofpixels:latest

# 4. Deploy to ECS
aws ecs update-service \
  --cluster nightofpixels \
  --service nightofpixels-service \
  --force-new-deployment \
  --region us-east-1

# 5. Monitor deployment
aws logs tail /ecs/nightofpixels --follow --region us-east-1
```

---

## 📊 Monitoring

### View Service Status
```bash
aws ecs describe-services \
  --cluster nightofpixels \
  --services nightofpixels-service \
  --region us-east-1
```

### Check Target Health
```bash
aws elbv2 describe-target-health \
  --target-group-arn arn:aws:elasticloadbalancing:us-east-1:YOUR_AWS_ACCOUNT_ID:targetgroup/nightofpixels-tg/3c224261bde3b00d \
  --region us-east-1
```

### View Logs
```bash
# Real-time logs
aws logs tail /ecs/nightofpixels --follow --region us-east-1

# Last 10 minutes
aws logs tail /ecs/nightofpixels --since 10m --region us-east-1
```

---

## 🔒 Security Configuration

### Security Groups
1. **ALB Security Group** (sg-09d97c21b51f9ebfa)
   - Inbound: HTTP (80) from 0.0.0.0/0

2. **ECS Security Group** (sg-00e4669a9bca0c6b6)
   - Inbound: HTTP (3000) from ALB security group

3. **RDS Security Group** (sg-0d1afb65586d5b9e8)
   - Inbound: PostgreSQL (5432) from ECS security group

### Secrets Manager
- `nightofpixels/database_url` - PostgreSQL connection string
- `nightofpixels/cookie_secret` - Session cookie secret

### IAM Role
- **ecsTaskExecutionRole** - Permissions for ECS to pull images and read secrets

---

## 🌐 Domain Configuration

### Cloudflare Setup
- **Domain**: eraith.dev
- **Subdomain**: pixels
- **DNS Record**: CNAME → `nightofpixels-alb-178048638.us-east-1.elb.amazonaws.com`
- **Proxy Status**: Proxied (orange cloud)
- **SSL Mode**: Flexible (HTTPS to Cloudflare, HTTP to origin)

---

## 💰 Monthly Cost Estimate

| Service | Configuration | Est. Cost |
|---------|--------------|-----------|
| ECS Fargate | 2 tasks, 0.5 vCPU, 1GB RAM each | ~$30 |
| RDS PostgreSQL | db.t3.micro | ~$15 |
| Application Load Balancer | Standard | ~$16 |
| ECR | Image storage | <$1 |
| Secrets Manager | 2 secrets | ~$1 |
| Data Transfer | Variable | ~$5-10 |
| **Total** | | **~$67-73/month** |

---

## 🔍 Troubleshooting

### Tasks Not Starting
```bash
# Check logs
aws logs tail /ecs/nightofpixels --since 10m --region us-east-1

# Check service events
aws ecs describe-services \
  --cluster nightofpixels \
  --services nightofpixels-service \
  --region us-east-1 \
  --query 'services[0].events[0:5]'
```

### ALB Returns 502/503
1. Verify targets are healthy
2. Check security groups allow traffic
3. Review CloudWatch logs for errors

### Database Connection Issues
1. Verify security group allows ECS → RDS on port 5432
2. Check DATABASE_URL secret is correct
3. Confirm RDS instance is running

---

## 📚 Key Files

- [Dockerfile](Dockerfile) - Container configuration with OpenSSL 3
- [prisma/schema.prisma](prisma/schema.prisma) - Database schema (PostgreSQL)
- [DEPLOYMENT-COMPLETE.md](DEPLOYMENT-COMPLETE.md) - Full deployment guide
- [.dockerignore](.dockerignore) - Files excluded from Docker build
- [docker-compose.yml](docker-compose.yml) - Local development setup

---

## 🎯 Next Steps (Optional)

1. **Enable HTTPS on ALB**
   - Request ACM certificate for pixels.eraith.dev
   - Add HTTPS listener to ALB
   - Update Cloudflare SSL to "Full"

2. **Auto-scaling**
   - Configure ECS service auto-scaling based on CPU/memory
   - Set min: 2, max: 10 tasks

3. **Database Backups**
   - Enable automated RDS snapshots
   - Configure backup retention period

4. **Monitoring & Alerts**
   - Set up CloudWatch alarms for:
     - High CPU usage (>80%)
     - Unhealthy targets
     - High response time (>2s)

5. **CI/CD Pipeline**
   - Set up GitHub Actions for automated deployments
   - Add automated tests before deployment

---

## ✅ Deployment Checklist

- [x] Docker image built for AMD64 platform
- [x] Image pushed to ECR
- [x] ECS cluster and service created
- [x] RDS PostgreSQL database provisioned
- [x] Database schema migrated
- [x] Secrets stored in Secrets Manager
- [x] Security groups configured
- [x] Application Load Balancer set up
- [x] Custom domain configured with Cloudflare
- [x] HTTPS enabled via Cloudflare
- [x] Admin user created
- [x] Application tested and verified working

---

**🎊 Congratulations! Your application is live!**

Access it at: **https://pixels.eraith.dev**

For support or questions, check the logs first:
```bash
aws logs tail /ecs/nightofpixels --follow --region us-east-1
```
