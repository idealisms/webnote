import os
import unittest
import webtest

from google.appengine.ext import ndb
from google.appengine.ext import testbed

import main

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
