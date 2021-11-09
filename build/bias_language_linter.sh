#!/bin/bash
# Script to detect bias language
# Reference: https://github.com/splunk/biased-lang-linter/blob/main/README.md

# exit when any command fails
set -e

LINTER=https://github.com/splunk/biased-lang-linter.git
LOCAL_DIR=./.biaslanguage
REPO=$(pwd)

#Clone bias language linter if it's the first run
if [ ! -d "$LOCAL_DIR" ]; then
    echo "First time running - cloning linter."
    mkdir -p $LOCAL_DIR
    git clone $LINTER $LOCAL_DIR
fi

#Install Ripgrep requirements
if [ ! $(which rg) ]; then
  echo "Ripgrep is required - installing using brew"
  brew install ripgrep
fi

#Run the linter locally
cd $LOCAL_DIR
python3 run_json.py --path=$REPO
cd -
