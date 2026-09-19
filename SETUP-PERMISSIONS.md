# Setup IAM Permissions for Fargate Deployment

You created a new user - now let's give it the right permissions.

---

## Step 1: Go to IAM Console

1. Login to AWS Console (as admin/root user)
2. Go to **IAM** service
3. Click **Users** in the left menu
4. Find and click your new user

---

## Step 2: Attach Permissions Policy

### Option A: Quick & Easy (Attach AWS Managed Policies)

Click **Add permissions** → **Attach policies directly**

Search and attach these AWS managed policies:
- ✅ **AmazonECS_FullAccess** (for ECS/Fargate)
- ✅ **AmazonEC2ContainerRegistryFullAccess** (for ECR)
- ✅ **AmazonRDSFullAccess** (for database)
- ✅ **ElasticLoadBalancingFullAccess** (for ALB)
- ✅ **AmazonRoute53FullAccess** (for DNS)
- ✅ **AWSCertificateManagerFullAccess** (for SSL)
- ✅ **SecretsManagerReadWrite** (for secrets)
- ✅ **CloudWatchLogsFullAccess** (for logs)
- ✅ **IAMReadOnlyAccess** (to read/list roles)

Click **Add permissions**

### Option B: Custom Policy (More Restrictive)

If you want tighter control, create a custom policy:

1. Click **Add permissions** → **Create inline policy**
2. Click **JSON** tab
3. Paste this policy:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "ecr:*",
                "ecs:*",
                "rds:*",
                "elasticloadbalancing:*",
                "route53:*",
                "acm:*",
                "secretsmanager:*",
                "logs:*",
                "ec2:DescribeVpcs",
                "ec2:DescribeSubnets",
                "ec2:DescribeSecurityGroups",
                "ec2:CreateSecurityGroup",
                "ec2:AuthorizeSecurityGroupIngress",
                "ec2:AuthorizeSecurityGroupEgress",
                "iam:GetRole",
                "iam:PassRole",
                "iam:ListRoles",
                "application-autoscaling:*"
            ],
            "Resource": "*"
        }
    ]
}
```

4. Click **Review policy**
5. Name it: `FargateDeploymentPolicy`
6. Click **Create policy**

---

## Step 3: Create Access Keys for CLI

1. Still in your user's page, click **Security credentials** tab
2. Scroll to **Access keys**
3. Click **Create access key**
4. Purpose: **Command Line Interface (CLI)**
5. Click **Next** → **Create access key**
6. **SAVE BOTH:**
   - Access Key ID
   - Secret Access Key
7. Click **Done**

---

## Step 4: Configure AWS CLI with New User

```bash
# Configure AWS CLI with your new user
aws configure

# Enter the values:
AWS Access Key ID: [Your new Access Key ID]
AWS Secret Access Key: [Your new Secret Access Key]
Default region name: us-east-1
Default output format: json

# Test it works
aws sts get-caller-identity
# Should show your new user ARN
```

---

## Step 5: Verify Permissions

```bash
# Test ECR access
aws ecr describe-repositories --region us-east-1

# Should return empty list (not an error) or existing repos
# If you get "AccessDenied" - permissions aren't set correctly
```

---

## Step 6: Continue with Deployment

Once permissions are working, go back to **[DEPLOY.md](./DEPLOY.md)** and continue from:

**Part 2: Deploy Container → Step 3: Create ECR Repository**

---

## 🔐 Security Best Practices

After deployment is done, you can:

1. **Remove unused policies** - Only keep what you need
2. **Enable MFA** - Add multi-factor auth to the user
3. **Rotate access keys** - Change them every 90 days
4. **Use least privilege** - Remove permissions you don't need

---

## 🆘 Troubleshooting

**"Access Denied" errors?**
- Make sure policies are attached to your user
- Wait 1-2 minutes for IAM changes to propagate
- Check you're using the right AWS CLI profile

**Can't create access keys?**
- Each user can have max 2 access keys
- Delete old ones if needed

**Not sure which user to use?**
```bash
# Check current user
aws sts get-caller-identity
```

---

**Once permissions are set, return to DEPLOY.md and continue!** 🚀
