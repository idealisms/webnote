#!/bin/bash
source env/bin/activate
dev_appserver.py --support_datastore_emulator=False --log_level=debug --enable_console=yes --port 8080 $PWD
