#!/usr/bin/env bash
set -Eeo pipefail

profile_name=$1
assume_role_name=$2

if [ -z "$profile_name" ]; then
  echo "Profile name is required"
  exit 1
fi

chown steampipe:0 /home/steampipe/.steampipe/db/14.2.0/data/

# Copy temp ro .aws cred
cp -r /tmp/aws ~/.aws

# Run Generate AWS IAM Users report
echo "Running IAM generate-credential-report"
aws iam generate-credential-report --profile $profile_name

export STEAMPIPE_DATABASE_START_TIMEOUT=300
export AWS_PROFILE=$profile_name

# STEAMPIPE_INSTALL_DIR overrides the default steampipe directory of ~/.steampipe
if [ -z $STEAMPIPE_INSTALL_DIR ] ; then
  echo "STEAMPIPE_INSTALL_DIR not defined. Using the default."
  export STEAMPIPE_INSTALL_DIR=~/.steampipe
fi

if [ ! -d $STEAMPIPE_INSTALL_DIR ] ; then
  echo "STEAMPIPE_INSTALL_DIR: $STEAMPIPE_INSTALL_DIR doesn't exist. Creating it."
  mkdir -p ${STEAMPIPE_INSTALL_DIR}/config/
fi


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
   # Write the content to config/aws.spc
  cat <<EOF > config/aws.spc
    connection "aws" {
      plugin  = "aws"
      regions = ["*"]
      profile = "$profile_name"
    }
EOF
fi

# Sleep in background indefinitely  
sleep infinity

