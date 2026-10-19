# EKS Pod Identity Configuration for SQS Access (cart, orders, checkout)

data "aws_iam_policy_document" "eks_pod_identity_trust" {
  statement {
    effect = "Allow"
    principals {
      type        = "Service"
      identifiers = ["pods.eks.amazonaws.com"]
    }
    actions = [
      "sts:AssumeRole",
      "sts:TagSession"
    ]
  }
}

resource "aws_iam_role" "sqs_access" {
  name               = "${var.environment_name}-sqs-pod-identity"
  assume_role_policy = data.aws_iam_policy_document.eks_pod_identity_trust.json

  tags = module.tags.result
}

resource "aws_iam_policy" "sqs_access" {
  name        = "${var.environment_name}-sqs-access-policy"
  description = "Allows microservices (cart, orders, checkout) to produce and consume SQS messages"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "sqs:SendMessage",
          "sqs:SendMessageBatch",
          "sqs:ReceiveMessage",
          "sqs:DeleteMessage",
          "sqs:DeleteMessageBatch",
          "sqs:GetQueueAttributes",
          "sqs:GetQueueUrl",
          "sqs:ChangeMessageVisibility",
          "sqs:ChangeMessageVisibilityBatch"
        ]
        Resource = "*"
      }
    ]
  })

  tags = module.tags.result
}

resource "aws_iam_role_policy_attachment" "sqs_access" {
  role       = aws_iam_role.sqs_access.name
  policy_arn = aws_iam_policy.sqs_access.arn
}

# Pod Identity Association for cart service
resource "aws_eks_pod_identity_association" "carts" {
  cluster_name    = module.retail_app_eks.eks_cluster_id
  namespace       = "carts"
  service_account = "carts"
  role_arn        = aws_iam_role.sqs_access.arn

  tags = module.tags.result
}

resource "aws_eks_pod_identity_association" "cart_alt" {
  cluster_name    = module.retail_app_eks.eks_cluster_id
  namespace       = "cart"
  service_account = "cart"
  role_arn        = aws_iam_role.sqs_access.arn

  tags = module.tags.result
}

# Pod Identity Association for orders service
resource "aws_eks_pod_identity_association" "orders" {
  cluster_name    = module.retail_app_eks.eks_cluster_id
  namespace       = "orders"
  service_account = "orders"
  role_arn        = aws_iam_role.sqs_access.arn

  tags = module.tags.result
}

# Pod Identity Association for checkout service
resource "aws_eks_pod_identity_association" "checkout" {
  cluster_name    = module.retail_app_eks.eks_cluster_id
  namespace       = "checkout"
  service_account = "checkout"
  role_arn        = aws_iam_role.sqs_access.arn

  tags = module.tags.result
}
