output "vpc_id" { value = aws_vpc.lab.id }
output "internet_gateway_id" { value = aws_internet_gateway.lab.id }
output "security_group_id" { value = aws_security_group.lab.id }
output "iam_instance_profile" { value = aws_iam_instance_profile.sam.name }
output "route_table_id" { value = aws_route_table.rr.id }

output "nodes" {
  value = {
    for name, instance in aws_instance.rr : name => {
      name         = name
      role         = "rr"
      site         = "aws"
      ssh_user     = "ubuntu"
      instance_id  = instance.id
      interface_id = instance.primary_network_interface_id
      private_ip   = instance.private_ip
      public_ip    = instance.public_ip
    }
  }
}
