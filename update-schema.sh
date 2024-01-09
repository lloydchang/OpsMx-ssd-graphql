#!/bin/sh

AUTHHEADERNAME='X-Dgraph-AuthToken'
AUTHHEADER="${AUTHHEADERNAME}: ${AUTHTOKEN}"

show_help() {
  echo "Usage: $0 -u BASEURL -t AUTHTOKEN -a DGRAPHAUTH "
  echo "  -u    URL for the GraphQL endpoint"
  echo "  -t    Authentication token for dgraph"
  echo "  -a    Dgraph authorization object to be passed onto the schema"
  exit 1
}

while getopts "u:t:a:" opt
do
  case "$opt" in 
    u ) BASEURL="$OPTARG" ;;
    t ) AUTHTOKEN="$OPTARG" ;;
    a ) DGRAPHAUTH="$OPTARG" ;;
    ? ) show_help ;;
  esac
done

if [ -z "$BASEURL" ] || [ -z "$AUTHTOKEN" ] || [ -z "$DGRAPHAUTH" ] 
then 
  echo "Some or all of the arguments are empty"
  show_help
fi

go run *.go --in schema/ssd.graphql --authtoken "${AUTHTOKEN}" --url "${BASEURL}" --dgraphauth "${DGRAPHAUTH}" --wipe \
  && get-graphql-schema --header "${AUTHHEADERNAME}=${AUTHTOKEN}" $BASEURL/graphql > schema/ssd.fetched.graphql \
  && echo \
  && go run github.com/Khan/genqlient