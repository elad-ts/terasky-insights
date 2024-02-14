#!/bin/bash

if [ "$#" -ne 1 ]; then
    echo "Usage: $0 <commit message>"
    exit 1
fi

COMMIT_MESSAGE="$1"
git add .

if ! git diff-index --quiet HEAD --; then
   
    git commit -m "$COMMIT_MESSAGE"
    git push origin main
    LATEST_TAG=$(git describe --tags `git rev-list --tags --max-count=1`)
    NEW_TAG=$(echo $LATEST_TAG | awk -F. '{OFS="."; $NF++; print $0}')
    if [ -z "$NEW_TAG" ]; then
        NEW_TAG="v0.1.0"
    fi
    git tag $NEW_TAG
    git push origin $NEW_TAG
else
    echo "No changes to commit."
fi
