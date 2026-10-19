# Karpenter SQS Interruption Queue & EventBridge Rules for Spot Interruption Handling

resource "aws_sqs_queue" "karpenter_interruption" {
  name                      = "${var.environment_name}-karpenter"
  message_retention_seconds = 300
  sqs_managed_sse_enabled   = true

  tags = var.tags
}

resource "aws_sqs_queue_policy" "karpenter_interruption" {
  queue_url = aws_sqs_queue.karpenter_interruption.id

  policy = jsonencode({
    Version = "2012-10-17"
    Id      = "KarpenterInterruptionQueuePolicy"
    Statement = [
      {
        Sid       = "AllowEventBridgeToSendMessage"
        Effect    = "Allow"
        Principal = {
          Service = ["events.amazonaws.com", "sqs.amazonaws.com"]
        }
        Action    = "sqs:SendMessage"
        Resource  = aws_sqs_queue.karpenter_interruption.arn
      }
    ]
  })
}

# EventBridge rule: EC2 Spot Instance Interruption Warning (2-minute warning)
resource "aws_cloudwatch_event_rule" "spot_interruption" {
  name        = "${var.environment_name}-spot-interruption"
  description = "Captures EC2 Spot Instance Interruption Warning for Karpenter"

  event_pattern = jsonencode({
    source      = ["aws.ec2"]
    detail-type = ["EC2 Spot Instance Interruption Warning"]
  })

  tags = var.tags
}

resource "aws_cloudwatch_event_target" "spot_interruption_target" {
  rule      = aws_cloudwatch_event_rule.spot_interruption.name
  target_id = "KarpenterInterruptionQueueTarget"
  arn       = aws_sqs_queue.karpenter_interruption.arn
}

# EventBridge rule: EC2 Instance Rebalance Recommendation
resource "aws_cloudwatch_event_rule" "rebalance_recommendation" {
  name        = "${var.environment_name}-rebalance-recommendation"
  description = "Captures EC2 Instance Rebalance Recommendation for Karpenter"

  event_pattern = jsonencode({
    source      = ["aws.ec2"]
    detail-type = ["EC2 Instance Rebalance Recommendation"]
  })

  tags = var.tags
}

resource "aws_cloudwatch_event_target" "rebalance_target" {
  rule      = aws_cloudwatch_event_rule.rebalance_recommendation.name
  target_id = "KarpenterRebalanceQueueTarget"
  arn       = aws_sqs_queue.karpenter_interruption.arn
}

# EventBridge rule: EC2 Instance State-change Notification
resource "aws_cloudwatch_event_rule" "instance_state_change" {
  name        = "${var.environment_name}-instance-state-change"
  description = "Captures EC2 Instance State-change Notification for Karpenter"

  event_pattern = jsonencode({
    source      = ["aws.ec2"]
    detail-type = ["EC2 Instance State-change Notification"]
  })

  tags = var.tags
}

resource "aws_cloudwatch_event_target" "instance_state_change_target" {
  rule      = aws_cloudwatch_event_rule.instance_state_change.name
  target_id = "KarpenterInstanceStateQueueTarget"
  arn       = aws_sqs_queue.karpenter_interruption.arn
}
