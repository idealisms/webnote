"""Checks a sample of real Notes revisions against the notes-handling code.

Needs a sample of raw Notes entities (user data, not in the repo):
  TOKEN=$(gcloud auth print-access-token) env/bin/python \\
      tools/dump_workspaces.py .data/workspaces.jsonl
  TOKEN=$(gcloud auth print-access-token) env/bin/python \\
      tools/sample_notes.py .data/workspaces.jsonl .data/notes_sample.jsonl
  python -m unittest notes_datatest

Set NOTES_SAMPLE_LIMIT to check only the first N revisions.

Stored formats (2026-10 sample): every revision is zlib-compressed JSON in an
unindexed blob. Revisions written by ndb carry meaning 22 (ndb's compressed
JsonProperty). Revisions saved before 2025-08-13 lost that meaning when the
dataflow job that added time_to_delete rewrote them, so they load as plain bytes
and models.get_notes_list() decompresses them itself. They expire by ~2027-08.
"""

import base64
import datetime
import json
import os
import shutil
import subprocess
import unittest
import zlib
import webtest

from google.appengine.api import datastore
from google.appengine.api import datastore_types
from google.appengine.ext import ndb
from google.appengine.ext import testbed

import main
import models

HERE = os.path.dirname(os.path.abspath(__file__))
SAMPLE = os.path.join(HERE, '.data', 'notes_sample.jsonl')
ZLIB_MEANING = 22
# Client attribute names for each note key (see Note.prototype.toXML).
XML_ATTRS = dict(zip(models.Notes.JSKEYS, models.Notes.DBKEYS))


def load_sample():
    limit = int(os.environ.get('NOTES_SAMPLE_LIMIT', '0')) or None
    records = []
    with open(SAMPLE) as f:
        for line in f:
            record = json.loads(line)
            record['raw'] = base64.b64decode(record['value_b64'])
            record['notes'] = json.loads(zlib.decompress(record['raw']))
            records.append(record)
            if limit and len(records) >= limit:
                break
    return records


def js_resave_texts(texts):
    """Returns escape(unescape(text)) for each text, as the client computes it.

    The page shows unescape(text), and the next save sends escape(shown text)
    (the native escape(), since 6f3776b).
    """
    script = """
        const fs = require('fs');
        const texts = JSON.parse(fs.readFileSync(0, 'utf8'));
        console.log(JSON.stringify(texts.map(t => escape(unescape(t)))));
    """
    output = subprocess.run(['node', '-e', script], input=json.dumps(texts),
                            check=True, capture_output=True, text=True).stdout
    return json.loads(output)


def client_xml(name, notes):
    """Builds the save request body the way webnote.js does."""
    ret = "<workspace name='%s' nextNoteNum='1'>\n" % name
    for note in notes:
        attrs = ' '.join("%s='%s'" % (XML_ATTRS[k], v)
                         for k, v in note.items() if k != 'text')
        ret += '<note %s>\n%s\n</note>\n' % (attrs, note.get('text', ''))
    return ret + '</workspace>'


@unittest.skipUnless(os.path.exists(SAMPLE), 'needs .data/notes_sample.jsonl')
class NotesSampleTest(unittest.TestCase):

    @classmethod
    def setUpClass(cls):
        cls.records = load_sample()

    def setUp(self):
        self.testbed = testbed.Testbed()
        self.testbed.activate()
        self.testbed.init_datastore_v3_stub()
        self.testbed.init_memcache_stub()
        ndb.get_context().set_cache_policy(False)
        self.app = webtest.TestApp(main.app, extra_environ={
            'APPLICATION_ID': os.environ['APPLICATION_ID']})

    def tearDown(self):
        self.testbed.deactivate()

    def clear_datastore(self, i):
        # The datastore stub scans every entity on each query, so keep it small.
        if i % 100 == 0:
            self.testbed.get_stub(testbed.DATASTORE_SERVICE_NAME).Clear()

    def testStoredFormats(self):
        for r in self.records:
            self.assertEqual('bytes', r['type'])
            self.assertFalse(r['indexed'])
            self.assertIn(r['meaning'], (ZLIB_MEANING, None))
            self.assertEqual([], r['other_properties'])

    def testNotesShape(self):
        for r in self.records:
            self.assertIsInstance(r['notes'], list)
            for note in r['notes']:
                self.assertIsInstance(note, dict)
                self.assertLessEqual(set(note), set(models.Notes.JSKEYS))
                self.assertIn('id', note)
                for key, value in note.items():
                    self.assertIsInstance(value, str, key)
                    if key != 'text':
                        # Attributes go into the save XML unescaped.
                        self.assertFalse(set(value) & set("'\"<>&"), value)

    def put_as_stored(self, record, workspace):
        """Puts a Notes entity in the same format as the stored one."""
        time = datetime.datetime.fromisoformat(record['time']).replace(tzinfo=None)
        if record['meaning'] == ZLIB_MEANING:
            # ndb writes zlib-compressed JSON with meaning 22.
            models.Notes(workspaceKey=workspace.key, time=time,
                         notesJsonArray=record['notes']).put()
        else:
            entity = datastore.Entity('Notes')
            entity['workspaceKey'] = workspace.key.to_old_key()
            entity['time'] = time
            entity['notesJsonArray'] = datastore_types.Blob(record['raw'])
            entity.set_unindexed_properties(['notesJsonArray'])
            datastore.Put(entity)
        return time

    def testAppReadsStoredRevisions(self):
        for i, r in enumerate(self.records):
            self.clear_datastore(i)
            workspace = models.Workspace.create('w%d' % i, 1, datetime.datetime(2000, 1, 1))
            workspace.time = self.put_as_stored(r, workspace)
            workspace.put()
            with self.assertNoLogs(level='ERROR'):
                self.assertEqual(r['notes'], workspace.get_notes_list(None),
                                 (r['workspace'], r['time']))

    @unittest.skipUnless(shutil.which('node'), 'needs node')
    def testSaveRoundTrip(self):
        texts = [n.get('text', '') for r in self.records for n in r['notes']]
        resaved = iter(js_resave_texts(texts))
        for i, r in enumerate(self.records):
            self.clear_datastore(i)
            expected = []
            for note in r['notes']:
                note = dict(note)
                note['text'] = next(resaved)
                expected.append(note)
            name = 'w%d' % i
            self.app.post('/webnote/save.py', client_xml(name, expected))

            workspace = models.Workspace.get_by_wsName(name)
            stored = models.Notes.query(
                models.Notes.workspaceKey == workspace.key).get()
            context = (r['workspace'], r['time'])
            self.assertEqual(expected, stored.notesJsonArray, context)

            raw = datastore.Get(stored.key.to_old_key())['notesJsonArray']
            self.assertEqual(expected, json.loads(zlib.decompress(raw)), context)

            page = self.app.get('/webnote/' + name).text
            notes_json = page.split('var notes = ', 1)[1].split(';\n', 1)[0]
            self.assertEqual(expected, json.loads(notes_json), context)

    @unittest.skipUnless(shutil.which('node'), 'needs node')
    def testResaveKeepsShownText(self):
        # What the user sees (unescape(text)) must not change across a save.
        texts = [n['text'] for r in self.records for n in r['notes'] if 'text' in n]
        script = """
            const fs = require('fs');
            const texts = JSON.parse(fs.readFileSync(0, 'utf8'));
            console.log(JSON.stringify(texts.filter(
                t => unescape(escape(unescape(t))) !== unescape(t)).length));
        """
        output = subprocess.run(['node', '-e', script], input=json.dumps(texts),
                                check=True, capture_output=True, text=True).stdout
        self.assertEqual(0, json.loads(output))


if __name__ == '__main__':
    unittest.main()
