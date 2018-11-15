# aws

## Deploying

Deployment is done using CloudFormation.

The CloudFormation stack will have a few outputs that your origin will require:
    * `OriginSecurityGroup` – The origin must be made a member of this security group.
    * `SecurityGroup` – The WebSocket service's security group. Your origin must allow ingress for this security group.
    * `ServiceURL` – This is the URL that the origin must use to submit service requests.
    * `WebsocketTargetGroup` – This is the target group that WebSocket traffic should be forwarded to from your load balancer.
    * `WebsocketLoadBalancerSecurityGroup` – The load balancer that sends traffic to `WebsocketTargetGroup` must be in this security group.
