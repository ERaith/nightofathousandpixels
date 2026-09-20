# Deploy to AWS Fargate - Night of a Thousand Pixels

**The simplest Fargate deployment guide.**

**Time:** 60 minutes
**Cost:** ~$30-50/month (only pay when running)
**Why:** Auto-scales, zero server management, high availability

---

## Prerequisites

- AWS account (eraith credentials)
- Domain: `eraith.dev` in Route53
- AWS CLI installed locally
- Docker installed locally

---

## Part 1: Setup (15 min)

### 1. Install AWS CLI

```bash
# macOS
brew install awscli

# Configure with eraith credentials
aws configure
# AWS Access Key ID: [from eraith account]
# AWS Secret Access Key: [from eraith account]
# Default region: us-east-1
# Default output format: json
```

### 2. Create PostgreSQL Database

**Via AWS Console:**

1. Go to **RDS** → **Create database**
2. Settings:
   - Engine: **PostgreSQL 16**
   - Template: **Production**
   - DB instance: **db.t4g.micro** ($15/month)
   - Storage: **20 GB**
   - DB name: `nightofpixels`
   - Username: `postgres`
   - Password: **[Create strong password - SAVE THIS!]**
   - Public access: **No**
   - Initial database: `nightofpixels`

3. Wait 5 minutes for creation
4. Copy the **Endpoint** (like `xxx.us-east-1.rds.amazonaws.com`)
nightofpixels.cj222qqs69nz.us-west-2.rds.amazonaws.com
---

## Part 2: Deploy Container (20 min)

### 3. Create ECR Repository

```bash
# Create image repository
aws ecr create-repository --repository-name nightofpixels --region us-east-1

# Save the URI (looks like: 123456789012.dkr.ecr.us-east-1.amazonaws.com/nightofpixels)
ECR_URI=$(aws ecr describe-repositories --repository-names nightofpixels --query 'repositories[0].repositoryUri' --output text)
echo $ECR_URI
```
{
    "repositories": [
        {
            "repositoryArn": "arn:aws:ecr:us-east-1:YOUR_AWS_ACCOUNT_ID:repository/nightofpixels",
            "registryId": "YOUR_AWS_ACCOUNT_ID",
            "repositoryName": "nightofpixels",
            "repositoryUri": "YOUR_AWS_ACCOUNT_ID.dkr.ecr.us-east-1.amazonaws.com/nightofpixels",
            "createdAt": "2025-10-27T20:49:46.278000-06:00",
            "imageTagMutability": "MUTABLE",
            "imageScanningConfiguration": {
                "scanOnPush": false
            },
            "encryptionConfiguration": {
                "encryptionType": "AES256"
            }
        }
    ]
}
### 4. Build and Push Docker Image

```bash
# Navigate to your project
cd /Users/homer/Documents/nightOf1000Pixels

# Login to ECR
aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin $ECR_URI

# Build image
docker build -t nightofpixels:latest .

# Tag image
docker tag nightofpixels:latest $ECR_URI:latest

# Push to ECR
docker push $ECR_URI:latest
```

### 5. Create ECS Cluster

```bash
# Create cluster
aws ecs create-cluster --cluster-name nightofpixels --region us-east-1
```

```
{
    "cluster": {
        "clusterArn": "arn:aws:ecs:us-east-1:YOUR_AWS_ACCOUNT_ID:cluster/nightofpixels",
        "clusterName": "nightofpixels",
        "status": "ACTIVE",
        "registeredContainerInstancesCount": 0,
        "runningTasksCount": 0,
        "pendingTasksCount": 0,
        "activeServicesCount": 0,
        "statistics": [],
        "tags": [],
        "settings": [
            {
                "name": "containerInsights",
                "value": "disabled"
            }
        ],
        "capacityProviders": [],
        "defaultCapacityProviderStrategy": []
    }
}
```

---

## Part 3: Configure Secrets (5 min)

### 6. Store Secrets

```bash
# Generate cookie secret
COOKIE_SECRET=$(openssl rand -hex 32)
dbcdadf8e9557690927d83c94fea9dbe7842b181420c51d2ad1fd184f6e8d8ad

# Create database URL secret
aws secretsmanager create-secret \
    --name nightofpixels/database_url \
    --secret-string "postgresql://postgres:__REDACTED_ROTATE_THIS__@nightofpixels.ca3q6uyo6ces.us-east-1.rds.amazonaws.com:5432/nightofpixels" \
    --region us-east-1

```
{
    "ARN": "arn:aws:secretsmanager:us-east-1:YOUR_AWS_ACCOUNT_ID:secret:nightofpixels/database_url-tSDtXx",
    "Name": "nightofpixels/database_url",
    "VersionId": "36216898-3ed8-480f-808b-fee7b164a884"
}
```
# Create cookie secret
aws secretsmanager create-secret \
    --name nightofpixels/cookie_secret \
    --secret-string "$COOKIE_SECRET" \
    --region us-east-1
```

```
{
    "ARN": "arn:aws:secretsmanager:us-east-1:YOUR_AWS_ACCOUNT_ID:secret:nightofpixels/cookie_secret-ywGPXv",
    "Name": "nightofpixels/cookie_secret",
    "VersionId": "359c6f95-12ac-4c2e-bfe7-f1c7e3de0946"
}
```
**Replace:**
- `YOUR_DB_PASSWORD` - Your RDS password
- `YOUR_RDS_ENDPOINT` - Your RDS endpoint (without port)

---

## Part 4: Configure via AWS Console (20 min)

This is easier in the console. Follow these steps:

### 7. Create Task Definition

1. Go to **ECS** → **Task Definitions** → **Create new task definition**
2. Task definition family: `nightofpixels`
3. Launch type: **AWS Fargate**
4. Operating system: **Linux/X86_64**
5. Task size:
   - CPU: **0.5 vCPU**
   - Memory: **1 GB**

6. Task execution role: **ecsTaskExecutionRole** (create if doesn't exist)

7. Container - 1:
   - Name: `nightofpixels-app`
   - Image URI: `[Your ECR URI from step 3]:latest`
   - Port: `3000`
   - Environment variables:
     ```
     NODE_ENV = production
     NEXT_PUBLIC_SITE_NAME = Night of a Thousand Pixels
     ORIGIN = https://pixels.eraith.dev
     ```
   - Secrets (from Secrets Manager):
     ```
     DATABASE_URL → nightofpixels/database_url
     COOKIE_SECRET → nightofpixels/cookie_secret
     ```

8. Logging: **Use CloudWatch Logs**
   - Log group: `/ecs/nightofpixels` (auto-create)

9. Click **Create**


// START HERE
### 8. Create Application Load Balancer

1. Go to **EC2** → **Load Balancers** → **Create Load Balancer**
2. Type: **Application Load Balancer**
3. Name: `nightofpixels-alb`
4. Scheme: **Internet-facing**
5. VPC: **Default**
6. Subnets: **Select 2+ availability zones**
7. Security group: **Create new**
   - Allow **HTTP (80)** from anywhere
   - Allow **HTTPS (443)** from anywhere

8. Target group:
   - Type: **IP addresses**
   - Name: `nightofpixels-tg`
   - Protocol: **HTTP**
   - Port: `3000`
   - Health check path: `/api/season`

9. Click **Create**

### 9. Create ECS Service

1. Go to **ECS** → **Clusters** → **nightofpixels**
2. **Services** → **Create**
3. Settings:
   - Launch type: **Fargate**
   - Task definition: `nightofpixels:latest`
   - Service name: `nightofpixels-service`
   - Number of tasks: **2**

4. Networking:
   - VPC: **Default**
   - Subnets: **All available**
   - Security group: **Create new**
     - Allow **port 3000** from ALB security group
     - Allow **port 5432** to RDS security group

5. Load balancing:
   - Type: **Application Load Balancer**
   - Select your ALB
   - Container: `nightofpixels-app:3000`
   - Target group: `nightofpixels-tg`

6. Click **Create**

7. Wait 3-5 minutes for tasks to start

---

## Part 5: Database Setup (5 min)

### 10. Run Migrations

```bash
# Get task ID
TASK_ARN=$(aws ecs list-tasks --cluster nightofpixels --service-name nightofpixels-service --query 'taskArns[0]' --output text)

# Get task details to find subnet and security group
aws ecs describe-tasks --cluster nightofpixels --tasks $TASK_ARN

# Run migration task (replace SUBNET and SG with values from above)
aws ecs run-task \
    --cluster nightofpixels \
    --task-definition nightofpixels \
    --launch-type FARGATE \
    --network-configuration "awsvpcConfiguration={subnets=[subnet-xxx],securityGroups=[sg-xxx],assignPublicIp=ENABLED}" \
    --overrides '{"containerOverrides":[{"name":"nightofpixels-app","command":["npx","prisma","migrate","deploy"]}]}'

# Wait 1 minute, then check CloudWatch logs for success
```

### 11. Create Admin User

```bash
# Run admin creation task
aws ecs run-task \
    --cluster nightofpixels \
    --task-definition nightofpixels \
    --launch-type FARGATE \
    --network-configuration "awsvpcConfiguration={subnets=[subnet-xxx],securityGroups=[sg-xxx],assignPublicIp=ENABLED}" \
    --overrides '{
      "containerOverrides":[{
        "name":"nightofpixels-app",
        "command":["node","-e","const bcrypt=require(\"bcryptjs\");const{PrismaClient}=require(\"@prisma/client\");const prisma=new PrismaClient();(async()=>{const email=\"admin@eraith.dev\";const password=\"ChangeMe123!\";const passwordHash=await bcrypt.hash(password,10);await prisma.adminUser.upsert({where:{email},update:{passwordHash},create:{email,passwordHash,name:\"Admin\"}});console.log(\"Admin created:\",email);await prisma.$disconnect();})();"]
      }]
    }'
```

**Admin credentials:**
- Email: `admin@eraith.dev`
- Password: `ChangeMe123!`

---

## Part 6: DNS & SSL (10 min)

### 12. Configure Route53

1. Get ALB DNS name:
   ```bash
   aws elbv2 describe-load-balancers --names nightofpixels-alb --query 'LoadBalancers[0].DNSName' --output text
   ```

2. Go to **Route53** → **Hosted Zones** → **eraith.dev**

3. **Create Record:**
   - Name: `pixels`
   - Type: **A - Alias**
   - Alias to: **Application Load Balancer**
   - Region: **us-east-1**
   - Select your ALB: `nightofpixels-alb`

4. Click **Create**

### 13. Add SSL Certificate

1. Go to **Certificate Manager** → **Request certificate**
2. Domain: `pixels.eraith.dev`
3. Validation: **DNS validation**
4. Click **Request**

5. Click certificate → **Create records in Route53** (button)

6. Wait 5-30 minutes for validation

7. Go to **EC2** → **Load Balancers** → Your ALB → **Listeners**

8. **Add listener:**
   - Protocol: **HTTPS**
   - Port: **443**
   - Default action: **Forward to** `nightofpixels-tg`
   - Certificate: **Select your certificate**

9. Click **Add**

---

## Part 7: Enable Auto-Scaling (5 min)

```bash
# Register scalable target
aws application-autoscaling register-scalable-target \
    --service-namespace ecs \
    --scalable-dimension ecs:service:DesiredCount \
    --resource-id service/nightofpixels/nightofpixels-service \
    --min-capacity 1 \
    --max-capacity 10 \
    --region us-east-1

# Create scaling policy
aws application-autoscaling put-scaling-policy \
    --service-namespace ecs \
    --scalable-dimension ecs:service:DesiredCount \
    --resource-id service/nightofpixels/nightofpixels-service \
    --policy-name cpu-scaling \
    --policy-type TargetTrackingScaling \
    --target-tracking-scaling-policy-configuration '{
      "TargetValue": 70.0,
      "PredefinedMetricSpecification": {
        "PredefinedMetricType": "ECSServiceAverageCPUUtilization"
      }
    }' \
    --region us-east-1
```

---

## 🎉 Done!

Your app is live at: **https://pixels.eraith.dev**

Admin: **https://pixels.eraith.dev/admin/login**

**Login and change password immediately!**

---

## 💰 Actual Costs

- **RDS db.t4g.micro:** ~$15/month
- **Fargate (2 tasks, 0.5 vCPU, 1GB):** ~$15-25/month
- **ALB:** ~$16/month
- **Data transfer:** ~$1-5/month
- **CloudWatch Logs:** ~$1/month

**Total:** ~$48-62/month when running 24/7

**Auto-scaling:** Scales down to 1 task during low traffic = lower cost

---

## 🔧 Maintenance

### View Logs

```bash
aws logs tail /ecs/nightofpixels --follow
```

### Update App

```bash
# Build and push new image
docker build -t nightofpixels:latest .
docker tag nightofpixels:latest $ECR_URI:latest
docker push $ECR_URI:latest

# Force new deployment
aws ecs update-service \
    --cluster nightofpixels \
    --service nightofpixels-service \
    --force-new-deployment
```

### Scale Service

```bash
# Scale to 4 tasks
aws ecs update-service \
    --cluster nightofpixels \
    --service nightofpixels-service \
    --desired-count 4
```

---

## 🆘 Troubleshooting

**Tasks not starting?**
```bash
# Check service events
aws ecs describe-services --cluster nightofpixels --services nightofpixels-service
```

**Can't connect to database?**
- Check RDS security group allows port 5432 from ECS security group
- Verify DATABASE_URL in Secrets Manager

**Health checks failing?**
- Check CloudWatch logs
- Verify app is listening on port 3000
- Check target group health in ALB console

---

**Need help?** Check CloudWatch logs first: `aws logs tail /ecs/nightofpixels --follow`

Good luck! 🚀


> curl -X POST https://pixels.eraith.dev/api/admin/create-first-admin \
  -H "Content-Type: application/json" \
  -d '{"email":"raithhtiar@gmail.com","password":"NIghtOfPixels2025!","name":"Admin"}'
