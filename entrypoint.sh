#!/usr/bin/env bash
set -Eeo pipefail

profile_name=$1
assume_role_name=$2

if [ -z "$profile_name" ]; then
  echo "Profile name is required"
  exit 1
fi

echo "Current user: $(id)"
echo "Environment variables:"
env

chown steampipe:0 /home/steampipe/.steampipe/db/14.2.0/data/

# Ensure ~/.aws exists and is owned by steampipe
mkdir -p /home/steampipe/.aws
chown steampipe:steampipe /home/steampipe/.aws

# Copy the AWS credentials and config, then change their ownership to steampipe
sudo cp -r /tmp/aws/* /home/steampipe/.aws/
sudo chown -R steampipe:steampipe /home/steampipe/.aws


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
  cat <<EOF > $STEAMPIPE_INSTALL_DIR/config/aws.spc
    connection "aws" {
      plugin  = "aws"
      regions = ["*"]
      profile = "$profile_name"
    }
EOF
fi

# Sleep in background indefinitely  
touch /tmp/ready && sleep infinity

