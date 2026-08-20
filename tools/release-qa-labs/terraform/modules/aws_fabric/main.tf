locals {
  common_tags = {
    ManagedBy         = var.managed_by
    Owner             = var.managed_by
    Project           = "routerd-sam-e2e"
    Environment       = "validation"
    ExpiresAt         = var.expires_at
    "routerd-purpose" = var.purpose
    "routerd-commit"  = var.commit
    "routerd-run-id"  = var.run_id
  }
}

# AWS provides the cloud leaf fabric only. Route reflectors run on PVE and no
# AWS RR instance, RR subnet, or RR route table is created here.
resource "aws_vpc" "lab" {
  cidr_block           = var.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = merge(local.common_tags, { Name = "routerd-${var.run_id}-aws" })
}

resource "aws_internet_gateway" "lab" {
  vpc_id = aws_vpc.lab.id
  tags   = merge(local.common_tags, { Name = "routerd-${var.run_id}-aws" })
}

resource "aws_security_group" "fabric" {
  name        = "routerd-${var.run_id}-aws"
  description = "routerd SAM E2E AWS leaf fabric ${var.run_id}"
  vpc_id      = aws_vpc.lab.id

  ingress {
    description = "ssh"
    protocol    = "tcp"
    from_port   = 22
    to_port     = 22
    cidr_blocks = var.ssh_cidr_blocks
  }

  ingress {
    description = "wireguard"
    protocol    = "udp"
    from_port   = 51820
    to_port     = 51820
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "icmp"
    protocol    = "icmp"
    from_port   = -1
    to_port     = -1
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "iperf3 public comparison"
    protocol    = "tcp"
    from_port   = 5201
    to_port     = 5201
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "iperf3 udp public comparison"
    protocol    = "udp"
    from_port   = 5201
    to_port     = 5201
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "same vpc"
    protocol    = "-1"
    from_port   = 0
    to_port     = 0
    cidr_blocks = [var.vpc_cidr]
  }

  egress {
    description = "all egress"
    protocol    = "-1"
    from_port   = 0
    to_port     = 0
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(local.common_tags, { Name = "routerd-${var.run_id}-aws" })
}

data "aws_iam_policy_document" "ec2_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "sam" {
  name               = "routerd-sam-e2e-${var.run_id}"
  assume_role_policy = data.aws_iam_policy_document.ec2_assume.json
  tags               = local.common_tags
}

data "aws_iam_policy_document" "sam_capture" {
  statement {
    effect = "Allow"
    actions = [
      "ec2:AssignPrivateIpAddresses",
      "ec2:UnassignPrivateIpAddresses",
      "ec2:DescribeInstances",
      "ec2:DescribeNetworkInterfaces",
      "ec2:DescribeRouteTables",
      "ec2:CreateRoute",
      "ec2:ReplaceRoute",
      "ec2:DeleteRoute",
      "ec2:ModifyNetworkInterfaceAttribute",
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "sam_capture" {
  name   = "routerd-sam-e2e-capture-${var.run_id}"
  role   = aws_iam_role.sam.id
  policy = data.aws_iam_policy_document.sam_capture.json
}

resource "aws_iam_instance_profile" "sam" {
  name = "routerd-sam-e2e-${var.run_id}"
  role = aws_iam_role.sam.name
  tags = local.common_tags
}
