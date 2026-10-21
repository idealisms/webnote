import datetime
import hashlib
import json
import unittest
import zlib

from google.appengine.api import datastore
from google.appengine.api import datastore_types
from google.appengine.ext import ndb
from google.appengine.ext import testbed

import models


class FakeRequest(object):
    def __init__(self, params):
        self.params = params

    def get(self, key, default=''):
        return self.params.get(key, default)


class ModelsTest(unittest.TestCase):

    def setUp(self):
        self.testbed = testbed.Testbed()
        self.testbed.activate()
        self.testbed.init_datastore_v3_stub()
        self.testbed.init_memcache_stub()
        ndb.get_context().clear_cache()

    def tearDown(self):
        self.testbed.deactivate()

    def put_workspace(self, name, time, next_note_num=0):
        workspace = models.Workspace.create(name, next_note_num, time)
        workspace.put()
        return workspace

    def put_notes(self, workspace, time, notes):
        return models.Notes(
            workspaceKey=workspace.key,
            time=time,
            notesJsonArray=notes).put()

    def testWorkspaceKeyIsSha1OfName(self):
        workspace = models.Workspace.create(
            'tc testing', 3, datetime.datetime(2017, 12, 31))
        self.assertEqual(
            hashlib.sha1(b'tc testing').hexdigest(), workspace.key.id())
        self.assertEqual('Workspace', workspace.key.kind())

    def testGetByWsName(self):
        self.put_workspace('tc testing', datetime.datetime(2017, 12, 31), 3)
        ndb.get_context().clear_cache()

        workspace = models.Workspace.get_by_wsName('tc testing')
        self.assertEqual('tc testing', workspace.name)
        self.assertEqual(3, workspace.nextNoteNum)
        self.assertEqual(datetime.datetime(2017, 12, 31), workspace.time)
        self.assertIsNone(models.Workspace.get_by_wsName('not found'))

    def testLocaltimeStr(self):
        # Times are stored as naive UTC and displayed in US/Eastern.
        winter = models.Workspace.create(
            'a', 0, datetime.datetime(2017, 12, 31, 5, 0, 0))
        self.assertEqual('2017-12-31 00:00:00', winter.localtime_str())
        summer = models.Notes(time=datetime.datetime(2017, 7, 4, 16, 30, 15))
        self.assertEqual('2017-07-04 12:30:15', summer.localtime_str())

    def testGetNotesListNoNotes(self):
        workspace = self.put_workspace('empty', datetime.datetime(2017, 12, 31))
        self.assertEqual([], workspace.get_notes_list(None))
        self.assertEqual([], workspace.get_notes_list(FakeRequest({})))

    def testGetNotesListLatest(self):
        old_time = datetime.datetime(2017, 12, 30)
        new_time = datetime.datetime(2017, 12, 31)
        workspace = self.put_workspace('ws', new_time)
        self.put_notes(workspace, old_time, [{'id': 'note0', 'text': 'old'}])
        self.put_notes(workspace, new_time, [{'id': 'note0', 'text': 'new'}])
        ndb.get_context().clear_cache()

        self.assertEqual([{'id': 'note0', 'text': 'new'}],
                         workspace.get_notes_list(FakeRequest({})))

    def testGetNotesListIgnoresOtherWorkspaces(self):
        # Regression test: two workspaces saved in the same second must not
        # see each other's notes.
        time = datetime.datetime(2017, 12, 31)
        ws_a = self.put_workspace('a', time)
        ws_b = self.put_workspace('b', time)
        self.put_notes(ws_a, time, [{'id': 'note0', 'text': 'from a'}])
        self.put_notes(ws_b, time, [{'id': 'note0', 'text': 'from b'}])
        ndb.get_context().clear_cache()

        self.assertEqual([{'id': 'note0', 'text': 'from a'}],
                         ws_a.get_notes_list(None))
        self.assertEqual([{'id': 'note0', 'text': 'from b'}],
                         ws_b.get_notes_list(None))

        ws_c = self.put_workspace('c', time)
        self.assertEqual([], ws_c.get_notes_list(None))

    def testGetNotesListAtTime(self):
        # The time parameter is US/Eastern local time.
        old_time = datetime.datetime(2017, 12, 30, 17, 0, 0)
        new_time = datetime.datetime(2017, 12, 31)
        workspace = self.put_workspace('ws', new_time)
        self.put_notes(workspace, old_time, [{'id': 'note0', 'text': 'old'}])
        self.put_notes(workspace, new_time, [{'id': 'note0', 'text': 'new'}])
        ndb.get_context().clear_cache()

        request = FakeRequest({'time': '2017-12-30 12:00:00'})
        self.assertEqual([{'id': 'note0', 'text': 'old'}],
                         workspace.get_notes_list(request))

        request = FakeRequest({'time': '2017-12-30 12:00:01'})
        self.assertEqual([], workspace.get_notes_list(request))

    def testGetNotesListBadTimeFallsBackToLatest(self):
        time = datetime.datetime(2017, 12, 31)
        workspace = self.put_workspace('ws', time)
        self.put_notes(workspace, time, [{'id': 'note0', 'text': 'new'}])
        ndb.get_context().clear_cache()

        request = FakeRequest({'time': 'not a time'})
        self.assertEqual([{'id': 'note0', 'text': 'new'}],
                         workspace.get_notes_list(request))

    def testGetNotesListUnicode(self):
        time = datetime.datetime(2017, 12, 31)
        workspace = self.put_workspace('ws', time)
        notes = [{'id': 'note0', 'text': 'café ☃ \U0001f600'}]
        self.put_notes(workspace, time, notes)
        ndb.get_context().clear_cache()

        self.assertEqual(notes, workspace.get_notes_list(None))

    def testGetNotesListLegacyCompressedBlob(self):
        # Some older entities store notesJsonArray as a plain blob of
        # zlib-compressed JSON (without ndb's compressed marker).
        time = datetime.datetime(2017, 12, 31)
        workspace = self.put_workspace('ws', time)
        notes = [{'id': 'note0', 'text': 'café'}]
        entity = datastore.Entity('Notes')
        entity['workspaceKey'] = workspace.key.to_old_key()
        entity['time'] = time
        entity['notesJsonArray'] = datastore_types.Blob(
            zlib.compress(json.dumps(notes).encode('utf8')))
        datastore.Put(entity)

        self.assertEqual(notes, workspace.get_notes_list(None))

    def testNotesStorageFormat(self):
        # Documents the raw Datastore format of a Notes entity, which other
        # clients (e.g., a Go port) need to read and write.
        workspace = self.put_workspace('ws', datetime.datetime(2017, 12, 31))
        notes = [{'id': 'note0', 'xPos': '10', 'text': 'café'}]
        key = models.Notes(
            workspaceKey=workspace.key,
            time=datetime.datetime(2017, 12, 31, 1, 2, 3),
            notesJsonArray=notes,
            time_to_delete=datetime.datetime(2019, 12, 31)).put()

        entity = datastore.Get(key.to_old_key())
        self.assertEqual('Notes', entity.kind())
        self.assertEqual(workspace.key.to_old_key(), entity['workspaceKey'])
        self.assertEqual(datetime.datetime(2017, 12, 31, 1, 2, 3),
                         entity['time'])
        self.assertEqual(datetime.datetime(2019, 12, 31),
                         entity['time_to_delete'])

        # notesJsonArray is an unindexed blob of zlib-compressed UTF-8 JSON.
        raw = entity['notesJsonArray']
        self.assertIsInstance(raw, bytes)
        self.assertIn('notesJsonArray', entity.unindexed_properties())
        self.assertEqual(notes, json.loads(zlib.decompress(raw).decode('utf8')))


if __name__ == '__main__':
    unittest.main()
