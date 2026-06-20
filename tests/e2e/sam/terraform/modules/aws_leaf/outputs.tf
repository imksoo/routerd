output "subnet_id" { value = aws_subnet.leaf.id }
output "route_table_id" { value = aws_route_table.leaf.id }

output "router" {
  value = {
    name         = local.router_name
    role         = "leaf"
    site         = "aws"
    ssh_user     = "ubuntu"
    instance_id  = aws_instance.router.id
    interface_id = aws_instance.router.primary_network_interface_id
    private_ip   = aws_instance.router.private_ip
    public_ip    = aws_instance.router.public_ip
  }
}

output "client" {
  value = {
    name         = local.client_name
    role         = "client"
    site         = "aws"
    ssh_user     = "ubuntu"
    instance_id  = aws_instance.client.id
    interface_id = aws_instance.client.primary_network_interface_id
    private_ip   = aws_instance.client.private_ip
    public_ip    = aws_instance.client.public_ip
  }
}

output "routers" {
  value = merge(
    {
      (local.router_name) = {
        name         = local.router_name
        role         = "leaf"
        site         = "aws"
        ssh_user     = "ubuntu"
        instance_id  = aws_instance.router.id
        interface_id = aws_instance.router.primary_network_interface_id
        private_ip   = aws_instance.router.private_ip
        public_ip    = aws_instance.router.public_ip
      }
    },
    {
      for name, instance in aws_instance.extra_router : name => {
        name         = name
        role         = "leaf"
        site         = "aws"
        ssh_user     = "ubuntu"
        instance_id  = instance.id
        interface_id = instance.primary_network_interface_id
        private_ip   = instance.private_ip
        public_ip    = instance.public_ip
      }
    }
  )
}

output "clients" {
  value = merge(
    {
      (local.client_name) = {
        name         = local.client_name
        role         = "client"
        site         = "aws"
        ssh_user     = "ubuntu"
        instance_id  = aws_instance.client.id
        interface_id = aws_instance.client.primary_network_interface_id
        private_ip   = aws_instance.client.private_ip
        public_ip    = aws_instance.client.public_ip
      }
    },
    {
      for name, instance in aws_instance.extra_client : var.extra_leaf_nodes[name].client_name => {
        name         = var.extra_leaf_nodes[name].client_name
        role         = "client"
        site         = "aws"
        ssh_user     = "ubuntu"
        instance_id  = instance.id
        interface_id = instance.primary_network_interface_id
        private_ip   = instance.private_ip
        public_ip    = instance.public_ip
      }
    }
  )
}
