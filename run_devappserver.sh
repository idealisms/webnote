#!/bin/bash
source env/bin/activate
dev_appserver.py --host 172.17.0.2 --port 8080 $PWD
