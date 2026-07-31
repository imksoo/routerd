locals {
  router_name = coalesce(var.router_name, var.name)
  client_name = coalesce(var.client_name, replace(var.name, "leaf", "client"))

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

  router_cloud_init = <<-CLOUD_INIT
    #cloud-config
    package_update: true
    packages: [curl, unzip]
    runcmd:
      - [bash, -lc, 'rm -rf /tmp/awscli-install /tmp/awscliv2.zip']
      - [bash, -lc, 'mkdir -p /tmp/awscli-install']
      - [bash, -lc, 'curl -fsSL https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip -o /tmp/awscliv2.zip']
      - [bash, -lc, 'unzip -q /tmp/awscliv2.zip -d /tmp/awscli-install']
      - [bash, -lc, '/tmp/awscli-install/aws/install --bin-dir /usr/local/bin --install-dir /usr/local/aws-cli --update']
      - [bash, -lc, 'aws --version']
      - [bash, -lc, 'rm -rf /tmp/awscli-install /tmp/awscliv2.zip']
  CLOUD_INIT
}

resource "aws_subnet" "leaf" {
  vpc_id                  = var.vpc_id
  cidr_block              = var.subnet_cidr
  map_public_ip_on_launch = true
  tags                    = merge(local.common_tags, { Name = "routerd-${var.run_id}-${var.name}" })
}

resource "aws_route_table" "leaf" {
  vpc_id = var.vpc_id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = var.internet_gateway_id
  }

  tags = merge(local.common_tags, { Name = "routerd-${var.run_id}-${var.name}" })
}

resource "aws_route_table_association" "leaf" {
  subnet_id      = aws_subnet.leaf.id
  route_table_id = aws_route_table.leaf.id
}

resource "aws_instance" "router" {
  ami                         = var.ami_id
  instance_type               = var.instance_type
  key_name                    = var.key_name
  subnet_id                   = aws_subnet.leaf.id
  private_ip                  = var.router_private_ip
  vpc_security_group_ids      = [var.security_group_id]
  iam_instance_profile        = var.iam_instance_profile
  associate_public_ip_address = true
  source_dest_check           = false
  user_data                   = local.router_cloud_init

  tags = merge(local.common_tags, {
    Name                 = "routerd-${var.run_id}-${local.router_name}"
    "routerd-node"       = local.router_name
    "routerd-role"       = "leaf"
    "cloudedge-mobility" = "false"
  })
}

resource "aws_instance" "client" {
  ami                         = var.ami_id
  instance_type               = var.client_instance_type
  key_name                    = var.key_name
  subnet_id                   = aws_subnet.leaf.id
  private_ip                  = var.client_private_ip
  vpc_security_group_ids      = [var.security_group_id]
  associate_public_ip_address = true
  source_dest_check           = true

  tags = merge(local.common_tags, {
    Name                 = "routerd-${var.run_id}-${local.client_name}"
    "routerd-node"       = local.client_name
    "routerd-role"       = "client"
    "cloudedge-mobility" = "true"
  })
}

resource "aws_instance" "extra_router" {
  for_each                    = var.extra_leaf_nodes
  ami                         = var.ami_id
  instance_type               = var.instance_type
  key_name                    = var.key_name
  subnet_id                   = aws_subnet.leaf.id
  private_ip                  = each.value.router_private_ip
  vpc_security_group_ids      = [var.security_group_id]
  iam_instance_profile        = var.iam_instance_profile
  associate_public_ip_address = true
  source_dest_check           = false
  user_data                   = local.router_cloud_init

  tags = merge(local.common_tags, {
    Name                 = "routerd-${var.run_id}-${each.key}"
    "routerd-node"       = each.key
    "routerd-role"       = "leaf"
    "cloudedge-mobility" = "false"
  })
}

resource "aws_instance" "extra_client" {
  for_each                    = var.extra_leaf_nodes
  ami                         = var.ami_id
  instance_type               = var.client_instance_type
  key_name                    = var.key_name
  subnet_id                   = aws_subnet.leaf.id
  private_ip                  = each.value.client_private_ip
  vpc_security_group_ids      = [var.security_group_id]
  associate_public_ip_address = true
  source_dest_check           = true

  tags = merge(local.common_tags, {
    Name                 = "routerd-${var.run_id}-${each.value.client_name}"
    "routerd-node"       = each.value.client_name
    "routerd-role"       = "client"
    "cloudedge-mobility" = "true"
  })
}
