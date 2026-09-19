#!/bin/bash
# Run the backpatch SQL to update votes with email addresses

echo "Running backpatch SQL on production database..."
echo "This will update 8 votes with the correct email addresses based on timestamp matching."
echo ""

# Get the task ARN
TASK_ARN=$(aws ecs list-tasks --cluster nightofpixels --service-name nightofpixels-service --region us-east-1 --query 'taskArns[0]' --output text)

if [ "$TASK_ARN" == "None" ] || [ -z "$TASK_ARN" ]; then
    echo "Error: Could not find running task"
    exit 1
fi

echo "Found task: $TASK_ARN"
echo ""
echo "Connecting to container and running SQL..."
echo ""

# Execute the SQL file via ECS exec
aws ecs execute-command \
    --cluster nightofpixels \
    --task "$TASK_ARN" \
    --container nightofpixels-container \
    --interactive \
    --command "sh -c 'cat scripts/backpatch-votes.sql | npx prisma db execute --stdin --schema prisma/schema.prisma'" \
    --region us-east-1

echo ""
echo "Backpatch complete!"
