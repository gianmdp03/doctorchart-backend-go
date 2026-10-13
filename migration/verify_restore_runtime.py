"""Simulated-provider helpers for current Go integration; historical Spring runner retired."""
import concurrent.futures
import json
import os
import secrets
import subprocess
import time
from fixture import ROOT,WORK,command,sql
from compare import schema,capture

def normalized_dump(cfg,db):
    text=command('docker','exec',cfg['container'],'pg_dump','-U','migration','-d',db,'--no-owner','--no-privileges').decode('utf-8')
    return '\n'.join(line for line in text.splitlines() if not line.startswith(('\\restrict','\\unrestrict')))+'\n'
