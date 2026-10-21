import datetime
import logging
from xml.dom.minidom import parseString

from google.appengine.api import datastore_errors
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
        except ValueError:
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
            text = node.firstChild.nodeValue if node.firstChild else ''
            note['text'] = text.strip()
            notesJsonArray.append(note)

        def txn():
            nowtime = datetime.datetime.now(datetime.timezone.utc).replace(
                tzinfo=None, microsecond=0)
            workspace = models.Workspace.get_by_wsName(wsName)
            if not workspace:
                workspace = models.Workspace.create(
                    wsName, nextNoteNum, nowtime)

            workspace.time = nowtime
            workspace.nextNoteNum = nextNoteNum
            workspace.put()
            models.Notes(
                workspaceKey=workspace.key,
                time=nowtime,
                notesJsonArray=notesJsonArray,
                time_to_delete=nowtime + datetime.timedelta(days=365*2)).put()
            return workspace

        self.response.content_type = 'text/xml'
        try:
            workspace = ndb.transaction(txn, xg=True, retries=5)
        except datastore_errors.TransactionFailedError:
            logging.exception('Failed to save workspace %s', wsName)
            self.response.status_int = 500
            self.response.write('<return><status value="error"/></return>')
            return

        # The client compares this against getrecent.py to detect saves from
        # other clients, so it must use the same format.
        self.response.write(
            '<return><status value="ok" update="%s"/></return>'
            % workspace.localtime_str())
