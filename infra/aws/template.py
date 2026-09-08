#!/usr/bin/env python3
"""Emit the small, dedicated demo stack. No ambient workload AWS credentials."""
import json

def ref(x): return {'Ref': x}
def attr(x, y): return {'Fn::GetAtt': [x, y]}
def sub(x): return {'Fn::Sub': x}
def resource(t, p, **kw): return {'Type': 'AWS::'+t, 'Properties': p, **kw}
def policy(statements): return {'Version':'2012-10-17','Statement':statements}
def allow(actions, targets): return {'Effect':'Allow','Action':actions,'Resource':targets}
def trust(service): return policy([{'Effect':'Allow','Principal':{'Service':service},'Action':'sts:AssumeRole'}])
r = {}
r['VPC'] = resource('EC2::VPC', {'CidrBlock':'10.77.0.0/16','EnableDnsSupport':True,'EnableDnsHostnames':True})
r['Gateway'] = resource('EC2::InternetGateway', {})
r['Attachment'] = resource('EC2::VPCGatewayAttachment', {'VpcId':ref('VPC'),'InternetGatewayId':ref('Gateway')})
r['Subnet'] = resource('EC2::Subnet', {'VpcId':ref('VPC'),'CidrBlock':'10.77.1.0/24','MapPublicIpOnLaunch':True})
r['Routes'] = resource('EC2::RouteTable', {'VpcId':ref('VPC')})
r['DefaultRoute'] = resource('EC2::Route', {'RouteTableId':ref('Routes'),'DestinationCidrBlock':'0.0.0.0/0','GatewayId':ref('Gateway')}, DependsOn='Attachment')
r['RouteAssociation'] = resource('EC2::SubnetRouteTableAssociation', {'RouteTableId':ref('Routes'),'SubnetId':ref('Subnet')})
r['S3Endpoint'] = resource('EC2::VPCEndpoint', {'VpcId':ref('VPC'),'ServiceName':sub('com.amazonaws.${AWS::Region}.s3'),'RouteTableIds':[ref('Routes')],'VpcEndpointType':'Gateway'})
r['Firewall'] = resource('EC2::SecurityGroup', {'GroupDescription':'Demo host: SSM only, no inbound ports','VpcId':ref('VPC'),'SecurityGroupEgress':[{'IpProtocol':'-1','CidrIp':'0.0.0.0/0'}]})
r['Key'] = resource('KMS::Key', {'Description':'Cleanup evidence encryption; retain until Object Lock expires','EnableKeyRotation':True,'KeyPolicy':policy([{'Effect':'Allow','Principal':{'AWS':sub('arn:${AWS::Partition}:iam::${AWS::AccountId}:root')},'Action':'kms:*','Resource':'*'}])}, DeletionPolicy='Retain', UpdateReplacePolicy='Retain')
r['Evidence'] = resource('S3::Bucket', {'BucketName':sub('cleanup-evidence-${AWS::AccountId}-${AWS::Region}'),'ObjectLockEnabled':True,'ObjectLockConfiguration':{'ObjectLockEnabled':'Enabled','Rule':{'DefaultRetention':{'Mode':'COMPLIANCE','Days':7}}},'VersioningConfiguration':{'Status':'Enabled'},'BucketEncryption':{'ServerSideEncryptionConfiguration':[{'ServerSideEncryptionByDefault':{'SSEAlgorithm':'aws:kms','KMSMasterKeyID':attr('Key','Arn')},'BucketKeyEnabled':False}]},'PublicAccessBlockConfiguration':{'BlockPublicAcls':True,'BlockPublicPolicy':True,'IgnorePublicAcls':True,'RestrictPublicBuckets':True},'LifecycleConfiguration':{'Rules':[{'Id':'BoundDemoRetention','Status':'Enabled','ExpirationInDays':8,'NoncurrentVersionExpiration':{'NoncurrentDays':8},'AbortIncompleteMultipartUpload':{'DaysAfterInitiation':1}}]}}, DeletionPolicy='Retain', UpdateReplacePolicy='Retain')
r['EvidencePolicy'] = resource('S3::BucketPolicy', {'Bucket':ref('Evidence'),'PolicyDocument':policy([{'Effect':'Deny','Principal':'*','Action':'s3:*','Resource':[attr('Evidence','Arn'),sub('${Evidence.Arn}/*')],'Condition':{'Bool':{'aws:SecureTransport':'false'}}}])})
r['Assets'] = resource('S3::Bucket', {'BucketName':sub('cleanup-deploy-${AWS::AccountId}-${AWS::Region}'),'BucketEncryption':{'ServerSideEncryptionConfiguration':[{'ServerSideEncryptionByDefault':{'SSEAlgorithm':'AES256'}}]},'PublicAccessBlockConfiguration':{'BlockPublicAcls':True,'BlockPublicPolicy':True,'IgnorePublicAcls':True,'RestrictPublicBuckets':True},'LifecycleConfiguration':{'Rules':[{'Id':'ExpireArtifacts','Status':'Enabled','ExpirationInDays':2}]}})
r['HostRole'] = resource('IAM::Role', {'AssumeRolePolicyDocument':trust('ec2.amazonaws.com'),'ManagedPolicyArns':['arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore'],'Policies':[{'PolicyName':'DemoAssetsAndSeparatedSessions','PolicyDocument':policy([allow(['s3:GetObject'],sub('${Assets.Arn}/*')),allow('sts:AssumeRole',[sub('arn:aws:iam::${AWS::AccountId}:role/${AWS::StackName}-writer'),sub('arn:aws:iam::${AWS::AccountId}:role/${AWS::StackName}-reader')])])}]})
for name, access in [('Writer',['s3:PutObject','s3:GetObject','s3:GetObjectVersion','s3:GetObjectRetention','s3:PutObjectRetention']),('Reader',['s3:GetObject','s3:GetObjectVersion','s3:GetObjectRetention'])]:
    r[name] = resource('IAM::Role', {'RoleName':sub('${AWS::StackName}-'+name.lower()),'MaxSessionDuration':3600,'AssumeRolePolicyDocument':policy([{'Effect':'Allow','Principal':{'AWS':attr('HostRole','Arn')},'Action':'sts:AssumeRole'}]),'Policies':[{'PolicyName':'EvidenceOnly','PolicyDocument':policy([allow(access,sub('${Evidence.Arn}/evidence/*')),{'Effect':'Allow','Action':['kms:Decrypt']+(['kms:GenerateDataKey'] if name=='Writer' else []),'Resource':attr('Key','Arn'),'Condition':{'StringEquals':{'kms:ViaService':sub('s3.${AWS::Region}.amazonaws.com')},'StringLike':{'kms:EncryptionContext:aws:s3:arn':sub('${Evidence.Arn}/evidence/*')}}}])}]})
r['Profile'] = resource('IAM::InstanceProfile', {'Roles':[ref('HostRole')]})
userdata = '''#!/bin/bash
set -euxo pipefail
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y ca-certificates curl git unzip python3 docker.io docker-compose-v2 docker-buildx conntrack jq make
curl -fL --retry 3 https://awscli.amazonaws.com/awscli-exe-linux-x86_64-2.36.40.zip -o /tmp/aws.zip
echo 'a904e314340ad4c4b62beb50e9259d4becb2acf3dffc86c96c553c834d7fdf7a  /tmp/aws.zip' | sha256sum -c -
unzip -q /tmp/aws.zip -d /tmp/cleanup-cli
/tmp/cleanup-cli/aws/install
systemctl enable --now docker
mkdir -p /opt/cleanup
chmod 700 /opt/cleanup
# Stop each boot after eight hours. EBS, retained S3 and KMS still incur small charges.
cat >/etc/systemd/system/cleanup-demo-stop.service <<'EOF'
[Unit]
Description=Stop demonstration host after bounded session
[Service]
Type=oneshot
ExecStart=/sbin/shutdown -h now
EOF
cat >/etc/systemd/system/cleanup-demo-stop.timer <<'EOF'
[Unit]
Description=Eight hour demonstration cost guard
[Timer]
OnBootSec=8h
[Install]
WantedBy=timers.target
EOF
systemctl daemon-reload
systemctl enable --now cleanup-demo-stop.timer
# Containers cannot retrieve host credentials, including Docker bridge traffic.
iptables -I DOCKER-USER -d 169.254.169.254/32 -j REJECT
touch /opt/cleanup/bootstrap-ready
'''
r['Host'] = resource('EC2::Instance', {'ImageId':ref('Image'),'InstanceType':'m7i-flex.large','IamInstanceProfile':ref('Profile'),'SubnetId':ref('Subnet'),'SecurityGroupIds':[ref('Firewall')],'MetadataOptions':{'HttpTokens':'required','HttpPutResponseHopLimit':1,'HttpEndpoint':'enabled','HttpProtocolIpv6':'disabled'},'BlockDeviceMappings':[{'DeviceName':'/dev/sda1','Ebs':{'VolumeSize':40,'VolumeType':'gp3','Encrypted':True,'DeleteOnTermination':True}}],'InstanceInitiatedShutdownBehavior':'stop','UserData':{'Fn::Base64':userdata},'Tags':[{'Key':'Name','Value':'cleanup-finals'},{'Key':'Purpose','Value':'hackathon-bounded-demo'}]}, DependsOn=['DefaultRoute','RouteAssociation'])
out = {x:{'Value':ref(x)} for x in ['Host','Evidence','Assets']}
out.update({x+'Arn':{'Value':attr(x,'Arn')} for x in ['Key','Writer','Reader']})
print(json.dumps({'AWSTemplateFormatVersion':'2010-09-09','Description':'Credit-funded cleanup receipt demo; no Firecracker claim','Parameters':{'Image':{'Type':'AWS::SSM::Parameter::Value<AWS::EC2::Image::Id>','Default':'/aws/service/canonical/ubuntu/server/24.04/stable/current/amd64/hvm/ebs-gp3/ami-id'}},'Resources':r,'Outputs':out},indent=2))
