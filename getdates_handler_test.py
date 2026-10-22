import datetime
import os
import unittest
import webtest

from google.appengine.ext import ndb
from google.appengine.ext import testbed

import main
import models


class GetDatesAndRecentTest(unittest.TestCase):
    """Tests getdates.py and getrecent.py, which the client polls."""

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

    def put_workspace(self, name, times):
        """Puts a workspace with one Notes revision per time (naive UTC)."""
        workspace = models.Workspace.create(name, 1, max(times))
        workspace.put()
        for time in times:
            models.Notes(workspaceKey=workspace.key, time=time,
                         notesJsonArray=[]).put()
        return workspace

    def testGetDatesPaging(self):
        # 15 revisions, one per hour, from 2017-12-31 05:00 UTC (00:00 EST).
        start = datetime.datetime(2017, 12, 31, 5)
        self.put_workspace('ws', [start + datetime.timedelta(hours=h)
                                  for h in range(15)])

        response = self.app.get('/webnote/getdates.py', {'name': 'ws'})
        self.assertEqual('text/plain', response.content_type)
        # Up to 11 dates, newest first, in US/Eastern. The client shows 10 and
        # uses the 11th to tell whether there are more.
        self.assertEqual(
            '|'.join('2017-12-31 %02d:00:00' % h for h in range(14, 3, -1)),
            response.text)

        response = self.app.get('/webnote/getdates.py',
                                {'name': 'ws', 'offset': '10'})
        self.assertEqual(
            '|'.join('2017-12-31 %02d:00:00' % h for h in range(4, -1, -1)),
            response.text)

        # A bad offset is treated as 0.
        response = self.app.get('/webnote/getdates.py',
                                {'name': 'ws', 'offset': 'abc'})
        self.assertTrue(response.text.startswith('2017-12-31 14:00:00|'))

    def testGetDatesListsSameSecondRevisions(self):
        time = datetime.datetime(2017, 12, 31, 5)
        self.put_workspace('ws', [time, time])
        response = self.app.get('/webnote/getdates.py', {'name': 'ws'})
        self.assertEqual('2017-12-31 00:00:00|2017-12-31 00:00:00',
                         response.text)

    def testGetDatesUnknownOrMissingName(self):
        for params in ({'name': 'missing'}, {'name': ''}, {}):
            response = self.app.get('/webnote/getdates.py', params)
            self.assertEqual(200, response.status_int)
            self.assertEqual('', response.text)

    def testGetRecent(self):
        # 2017-07-04 16:30:15 UTC is 12:30:15 EDT.
        self.put_workspace('ws', [datetime.datetime(2017, 7, 4, 16, 30, 15)])
        response = self.app.get('/webnote/getrecent.py', {'name': 'ws'})
        self.assertEqual('text/plain', response.content_type)
        self.assertEqual('2017-07-04 12:30:15', response.text)

    def testGetRecentUnknownOrMissingName(self):
        for params in ({'name': 'missing'}, {'name': ''}, {}):
            response = self.app.get('/webnote/getrecent.py', params)
            self.assertEqual(200, response.status_int)
            self.assertEqual('', response.text)


if __name__ == '__main__':
    unittest.main()
