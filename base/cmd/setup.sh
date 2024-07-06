#!/usr/bin/env bash

source cmd/helper_functions.sh
parse_db_params "$1"
ABOTPKG='github.com/itsabot/abot'

printf "\n* [starting]"
printf " ***********************************************************\n\n"

run "checking for go binary" "which go" "please make sure 'go' is in your path"
run "checking GOPATH" "[ -n '$GOPATH' ]" "GOPATH is not set"
run "installing dependency manager" "go get github.com/robfig/glock" \
run "checking for glock binary" "which glock" \
	"please make sure 'glock' is in your path"
run "syncing dependencies" "glock sync '$ABOTPKG'"
run "installing glock hook" "glock install '$ABOTPKG'"
run "installing abot" "go install '$ABOTPKG'"

which psql &>/dev/null
if [ $? -ne 0 ]; then
	put "warn" "psql binary not found. skipping database setup"
	put "" "if the database is setup and available you can ignore this message"
else
	cmd/dbsetup.sh "$1"
	[ $? -ne 0 ] && exit 1
fi

printf "\n* [finished]"
printf " ***********************************************************\n\n"

echo "to boot abot:
    1. run 'abot server'
    2. open a web browser to $ABOT_URL

you'll want to sign up to create a user account next"
