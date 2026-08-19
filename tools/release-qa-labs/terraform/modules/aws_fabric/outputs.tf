output "vpc_id" { value = aws_vpc.lab.id }
output "internet_gateway_id" { value = aws_internet_gateway.lab.id }
output "security_group_id" { value = aws_security_group.fabric.id }
output "iam_instance_profile" { value = aws_iam_instance_profile.sam.name }
