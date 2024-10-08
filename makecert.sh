#!/bin/bash
mkdir certs
rm certs/*
echo "make server cert"
openssl req -new -nodes -x509 -out certs/server.pem -keyout certs/server.key -days 3650 -subj "/C=KG/ST=Bishkek Company/OU=BARI/CN=bari312.storage.com/emailAddress=$1"
echo "make client cert"
openssl req -new -nodes -x509 -out certs/client.pem -keyout certs/client.key -days 3650 -subj "/C=KG/ST=Bishkek Company/OU=BARI/CN=bari312.storage.com/emailAddress=$1"
