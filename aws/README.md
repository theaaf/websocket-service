# aws

## Deploying

* Create a CloudFormation stack using websocket-service.cfn.yaml.
* The CloudFormation stack will have three outputs that your origin will require:
    * `OriginSecurityGroup` – The origin must be made a member of this security group.
    * `ServiceURL` – This is the URL that the origin should use to submit service requests.
    * `WebsocketTargetGroup` – This is the target group that WebSocket traffic should be forwarded to from your load balancer.
