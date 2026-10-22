import datetime
import os
import unittest
import webtest

from google.appengine.ext import ndb
from google.appengine.ext import testbed

import main
import models

class LoadHandlerTest(unittest.TestCase):

    def setUp(self):
        # First, create an instance of the Testbed class.
        self.testbed = testbed.Testbed()
        # Then activate the testbed, which prepares the service stubs for use.
        self.testbed.activate()
        # Next, declare which service stubs you want to use.
        # Fail queries that need an index missing from index.yaml.
        self.testbed.init_datastore_v3_stub(
            require_indexes=True,
            root_path=os.path.dirname(os.path.abspath(__file__)))
        self.testbed.init_memcache_stub()
        # Clear ndb's in-context cache between tests.
        # This prevents data from leaking between tests.
        # Alternatively, you could disable caching by
        # using ndb.get_context().set_cache_policy(False)
        ndb.get_context().clear_cache()

    def tearDown(self):
        self.testbed.deactivate()

    def testLoad(self):
        # main.app snapshots os.environ at import time, before the testbed
        # sets APPLICATION_ID, so pass it through the WSGI environ.
        app = webtest.TestApp(main.app, extra_environ={
            'APPLICATION_ID': os.environ['APPLICATION_ID']})

        response = app.get('/webnote/tc%252520testing')
        self.assertEqual(200, response.status_int)

    def get_page(self, path):
        app = webtest.TestApp(main.app, extra_environ={
            'APPLICATION_ID': os.environ['APPLICATION_ID']})
        return app.get(path).text

    def testFirefoxUrlLoadsSameWorkspace(self):
        time = datetime.datetime(2017, 12, 31)
        workspace = models.Workspace.create('my%2Dnotes', 1, time)
        workspace.put()
        models.Notes(workspaceKey=workspace.key, time=time,
                     notesJsonArray=[{'id': 'note0', 'text': 'hi'}]).put()

        # The URL escapes the stored name twice more: Chrome's form, then the
        # form an old Firefox bookmark has.
        for path in ('/webnote/my%25252Dnotes', '/webnote/my-notes'):
            self.assertIn('var notes = [{"id": "note0", "text": "hi"}];',
                          self.get_page(path), path)

    def testNonAsciiNameInUrl(self):
        # Used to fail with UnicodeEncodeError (a 500).
        self.assertIn('var notes = [];', self.get_page('/webnote/Bogot%C3%A1'))

    def testPageValues(self):
        # 2017-12-31 05:00 UTC is 00:00 EST.
        time = datetime.datetime(2017, 12, 31, 5)
        workspace = models.Workspace.create('my%20notes', 7, time)
        workspace.put()
        models.Notes(workspaceKey=workspace.key, time=time,
                     notesJsonArray=[{'id': 'note0', 'text': 'hi'}]).put()

        page = self.get_page('/webnote/my%252520notes')
        for line in ("debugOn = 0;",
                     "workspace.setName(unescape('my%20notes'));",
                     "workspace.loadedTime = '2017-12-31 00:00:00';",
                     "adminEmail = 'webnote@ponderer.org';",
                     "numDates = 10;",
                     'var notes = [{"id": "note0", "text": "hi"}];',
                     "workspace.nextNoteNum = 7;",
                     "<title> my%20notes</title>"):
            self.assertIn(line, page)

    def testNewWorkspacePage(self):
        page = self.get_page('/webnote/new')
        for line in ("workspace.loadedTime = '';",
                     'var notes = [];',
                     "workspace.nextNoteNum = 0;"):
            self.assertIn(line, page)

    def testLoadPyNameParam(self):
        # The snapshot form and the bookmarklet use load.py?name=, with the
        # stored name escaped once more by the form encoding.
        workspace = models.Workspace.create(
            'my%2Dnotes', 1, datetime.datetime(2017, 12, 31))
        workspace.put()
        page = self.get_page('/webnote/load.py?name=my%252Dnotes')
        self.assertIn("workspace.setName(unescape('my%2Dnotes'));", page)
        self.assertIn("workspace.nextNoteNum = 1;", page)

    def testLoadPyWithoutName(self):
        # Currently a 500; a 400 or a redirect to /webnote/ would be better.
        app = webtest.TestApp(main.app, extra_environ={
            'APPLICATION_ID': os.environ['APPLICATION_ID']})
        with self.assertLogs(level='ERROR'):
            app.get('/webnote/load.py', status=500)
