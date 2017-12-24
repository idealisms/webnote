import datetime
import logging
import time
from xml.dom.minidom import *

from google.appengine.ext import db
from google.appengine.ext import ndb
import webapp2

import models

class SaveHandler(webapp2.RequestHandler):
    def post(self):
        dom = parseString(self.request.body)
        wsRoot = dom.getElementsByTagName('workspace').item(0)
        wsName = wsRoot.getAttribute('name')
        try:
            nextNoteNum = int(wsRoot.getAttribute('nextNoteNum'))
        except:
            nextNoteNum = 0
        notesJsonArray = []

        nlNotes = wsRoot.getElementsByTagName('note')
        for i in range(nlNotes.length):
            node = nlNotes.item(i)
            note = {}
            for j in range(node.attributes.length):
                attr = node.attributes.item(j)

                if attr.name in models.Notes.DBKEYS: # only use valid attributes
                    note[models.Notes.DB2JS[attr.name]] = attr.nodeValue
            note['text'] = node.firstChild.nodeValue.strip()
            notesJsonArray.append(note)

        def txn():
            nowtime = datetime.datetime.now().replace(microsecond=0)
            workspace = models.Workspace.get_by_wsName(wsName)
            if not workspace:
                workspace = models.Workspace.create(
                    wsName, nextNoteNum, nowtime)
                workspace.put()

            workspace.time = nowtime
            workspace.nextNoteNum = nextNoteNum
            workspace.put()
            notes = models.Notes(
                workspaceKey=workspace.key,
                time=nowtime,
                notesJsonArray=notesJsonArray)
            notes.put()
        for i in range(5):
            try:
                ndb.transaction(txn, xg=True)
                break
            except db.TransactionFailedError:
                time.sleep(1.1)

        origin = self.request.headers.get('Origin', '')
        logging.info(origin)
        if origin in ('http://localhost:8080', 'http://www.aypwip.org', 'https://www.aypwip.org'):
            self.response.headers['Access-Control-Allow-Origin'] = origin

        self.response.content_type = 'text/xml'
        self.response.write(
            '<return><status value="ok" update="%s"/></return>'
            % '2017-11-01 12:12:12')
