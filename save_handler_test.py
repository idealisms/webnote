import datetime
import os
import unittest
from unittest import mock
import webtest

from google.appengine.api import datastore_errors
from google.appengine.ext import ndb
from google.appengine.ext import testbed

import main
import models


def make_payload(name, notes, next_note_num='1'):
    """Builds a save request body the way webnote.js does."""
    ret = "<workspace name='%s' nextNoteNum='%s'>\n" % (name, next_note_num)
    for attrs, text in notes:
        ret += '<note %s>\n%s\n</note>\n' % (
            ' '.join("%s='%s'" % kv for kv in attrs.items()), text)
    return ret + '</workspace>'


NOTE_ATTRS = {
    'noteid': 'note0',
    'bgcolor': '#ffff99',
    'xposition': '10',
    'yposition': '20',
    'height': '100',
    'width': '200',
    'zindex': '1',
}


class SaveHandlerTest(unittest.TestCase):

    def setUp(self):
        self.testbed = testbed.Testbed()
        self.testbed.activate()
        # Fail queries that need an index missing from index.yaml.
        self.testbed.init_datastore_v3_stub(
            require_indexes=True,
            root_path=os.path.dirname(os.path.abspath(__file__)))
        self.testbed.init_memcache_stub()
        ndb.get_context().clear_cache()
        # main.app snapshots os.environ at import time, before the testbed
        # sets APPLICATION_ID, so pass it through the WSGI environ.
        self.app = webtest.TestApp(main.app, extra_environ={
            'APPLICATION_ID': os.environ['APPLICATION_ID']})

    def tearDown(self):
        self.testbed.deactivate()

    def save(self, body, status=200):
        return self.app.post('/webnote/save.py', body, status=status)

    def testSaveNewWorkspace(self):
        response = self.save(make_payload(
            'my%20notes', [(NOTE_ATTRS, 'hello%20world')], next_note_num='3'))
        self.assertEqual('text/xml', response.content_type)

        # The name is stored as sent (escaped once by the client).
        workspace = models.Workspace.get_by_wsName('my%20notes')
        self.assertEqual('my%20notes', workspace.name)
        self.assertEqual(3, workspace.nextNoteNum)

        notes = models.Notes.query().fetch()
        self.assertEqual(1, len(notes))
        self.assertEqual(workspace.key, notes[0].workspaceKey)
        self.assertEqual(workspace.time, notes[0].time)
        self.assertEqual(workspace.time + datetime.timedelta(days=730),
                         notes[0].time_to_delete)
        self.assertEqual([{
            'id': 'note0',
            'bgcolor': '#ffff99',
            'xPos': '10',
            'yPos': '20',
            'height': '100',
            'width': '200',
            'zIndex': '1',
            'text': 'hello%20world',
        }], notes[0].notesJsonArray)
        self.assertEqual(notes[0].notesJsonArray, workspace.get_notes_list(None))

    def testSaveTimeIsUtc(self):
        before = datetime.datetime.utcnow().replace(microsecond=0)
        self.save(make_payload('ws', []))
        after = datetime.datetime.utcnow()

        workspace = models.Workspace.get_by_wsName('ws')
        self.assertLessEqual(before, workspace.time)
        self.assertLessEqual(workspace.time, after)
        self.assertEqual(0, workspace.time.microsecond)

    def testResponseUpdateMatchesGetRecent(self):
        # The client compares the update time against getrecent.py to detect
        # saves from other clients, so the two must match exactly.
        response = self.save(make_payload('ws', [(NOTE_ATTRS, 'text')]))
        recent = self.app.get('/webnote/getrecent.py', {'name': 'ws'})

        self.assertEqual(
            '<return><status value="ok" update="%s"/></return>' % recent.text,
            response.text)

    def testSaveExistingWorkspace(self):
        self.save(make_payload('ws', [(NOTE_ATTRS, 'first')], '1'))
        self.save(make_payload('ws', [(NOTE_ATTRS, 'second')], '2'))

        self.assertEqual(1, models.Workspace.query().count())
        self.assertEqual(2, models.Workspace.get_by_wsName('ws').nextNoteNum)
        texts = sorted(n.notesJsonArray[0]['text']
                       for n in models.Notes.query().fetch())
        self.assertEqual(['first', 'second'], texts)

    def testIgnoresUnknownAttributes(self):
        attrs = {'noteid': 'note0', 'onclick': 'evil()', 'id': 'x'}
        self.save(make_payload('ws', [(attrs, 'text')]))

        notes = models.Notes.query().get()
        self.assertEqual([{'id': 'note0', 'text': 'text'}],
                         notes.notesJsonArray)

    def testEmptyNoteAndBadNextNoteNum(self):
        body = ("<workspace name='ws' nextNoteNum='abc'>"
                "<note noteid='note0'></note></workspace>")
        self.save(body)

        self.assertEqual(0, models.Workspace.get_by_wsName('ws').nextNoteNum)
        self.assertEqual([{'id': 'note0', 'text': ''}],
                         models.Notes.query().get().notesJsonArray)

    def testNoNotes(self):
        self.save(make_payload('ws', []))
        self.assertEqual([], models.Notes.query().get().notesJsonArray)

    def testTransactionFailure(self):
        with mock.patch.object(
                ndb, 'transaction',
                side_effect=datastore_errors.TransactionFailedError()):
            with self.assertLogs(level='ERROR'):
                response = self.save(make_payload('ws', []), status=500)

        self.assertEqual('<return><status value="error"/></return>',
                         response.text)
        self.assertIsNone(models.Workspace.get_by_wsName('ws'))

    def testNoCorsHeader(self):
        response = self.app.post(
            '/webnote/save.py', make_payload('ws', []),
            headers={'Origin': 'https://www.aypwip.org'})
        self.assertNotIn('Access-Control-Allow-Origin', response.headers)


if __name__ == '__main__':
    unittest.main()
