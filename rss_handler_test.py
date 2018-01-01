import datetime
import hashlib
import unittest
import webtest

from google.appengine.api import memcache
from google.appengine.ext import ndb
from google.appengine.ext import testbed

import main
import models

class RssHandlerTest(unittest.TestCase):

    def setUp(self):
        # First, create an instance of the Testbed class.
        self.testbed = testbed.Testbed()
        # Then activate the testbed, which prepares the service stubs for use.
        self.testbed.activate()
        # Next, declare which service stubs you want to use.
        self.testbed.init_datastore_v3_stub()
        self.testbed.init_memcache_stub()
        # Clear ndb's in-context cache between tests.
        # This prevents data from leaking between tests.
        # Alternatively, you could disable caching by
        # using ndb.get_context().set_cache_policy(False)
        ndb.get_context().clear_cache()

    def tearDown(self):
        self.testbed.deactivate()

    def testRss(self):
        app = webtest.TestApp(main.app)

        app.get('/webnote/notfound.xml', status=404)

        ws_key = models.Workspace(
            id=hashlib.sha1('tc%20testing').hexdigest(),
            name='tc%20testing',
            nextNoteNum=3,
            time=datetime.datetime(2017, 12, 31)
        ).put()
        models.Notes(
            workspaceKey=ws_key,
            time=datetime.datetime(2017, 12, 31),
            notesJsonArray=[
                {'id': 'note0', 'text': 'This is note 0.'},
                {'id': 'note2', 'text': 'blah blah'},
                {},
            ]
        ).put()

        response = app.get('/webnote/tc%252520testing.xml', status=200)
        self.assertIn('<channel><title>tc testing</title>', response.body)
        self.assertIn('<link>http://localhost/webnote/tc%252520testing</link>', response.body)
        self.assertIn('<lastBuildDate>Sun, 31 Dec 2017 00:00:00 GMT</lastBuildDate>', response.body)
        self.assertIn('http://localhost/webnote/tc%252520testing#note2', response.body)
