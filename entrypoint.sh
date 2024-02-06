#!/usr/bin/env bash
set -Eeo pipefail

profile_name=$1
mod=$2
assume_role_name=$3

if [ -z "$profile_name" ]; then
  echo "Profile name is required"
  exit 1
fi

if [ -z "$mod" ]; then
  echo "Mod name is required"
  exit 1
fi

chown steampipe:0 /home/steampipe/.steampipe/db/14.2.0/data/

# Copy temp ro .aws cred
cp -r /tmp/aws ~/.aws

# Run Generate AWS IAM Users report
echo "Running IAM generate-credential-report"
aws iam generate-credential-report --profile $profile_name

export STEAMPIPE_DATABASE_START_TIMEOUT=300

# Run Find AWS Organization accounts and setup aws plugin auth config
if [ -n "$assume_role_name" ]; then

  echo "Assume role name $assume_role_name "

  /home/steampipe/generate_config_for_cross_account_roles.sh LOCAL $assume_role_name aws_all_accounts_config $profile_name
  echo -e "\n$(cat aws_all_accounts_config)" >> ~/.aws/config
  PROFILES=`grep '\[profile' ~/.aws/config  | awk '{print $2}' | sed s/\]//g`

  for p in $PROFILES ; do
    echo "Generating credential report in $p"
    aws iam generate-credential-report --profile $profile_name --output text
  done
  echo "Assume role"
else 
  export AWS_PROFILE=$profile_name
fi 

# Sleep in background indefinitely  
cd /mods/$mod && steampipe service start --dashboard
sleep infinity

