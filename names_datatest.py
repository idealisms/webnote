"""Checks every stored workspace name against the name-handling code.

Needs a dump of the Workspace table (user data, not in the repo):
  TOKEN=$(gcloud auth print-access-token) env/bin/python \\
      tools/dump_workspaces.py .data/workspaces.jsonl
  python -m unittest names_datatest
"""

import hashlib
import json
import os
import re
import shutil
import subprocess
import unittest
import webtest

from google.appengine.ext import ndb
from google.appengine.ext import testbed

import main
import models

HERE = os.path.dirname(os.path.abspath(__file__))
DUMP = os.path.join(HERE, '.data', 'workspaces.jsonl')

# myescape() writes U+0100-U+0FFF unpadded (%u5D9), which the native unescape()
# can't decode, so these names don't survive the page's unescape() and the next
# save's myescape(). Known and pre-existing; see canonical_ws_name().
UNPADDED_U = re.compile(r'%u[0-9A-F]{3}(?![0-9A-F])')


def load_names():
    with open(DUMP) as f:
        return [json.loads(line) for line in f]


@unittest.skipUnless(os.path.exists(DUMP), 'needs .data/workspaces.jsonl')
class StoredNamesTest(unittest.TestCase):

    @classmethod
    def setUpClass(cls):
        cls.workspaces = load_names()

    def testNamesAreCanonical(self):
        bad = [w['name'] for w in self.workspaces
               if models.canonical_ws_name(w['name']) != w['name']]
        self.assertEqual([], bad)

    def testKeysAreSha1OfName(self):
        bad = [w['name'] for w in self.workspaces
               if hashlib.sha1(w['name'].encode('ascii')).hexdigest() != w['key']]
        self.assertEqual([], bad)

    def testNamesUseOnlyEscapedCharacters(self):
        # Stored names are myescape() output: letters, digits, '_' and escapes.
        bad = [w['name'] for w in self.workspaces
               if not re.fullmatch(r'([A-Za-z0-9_]|%[0-9A-F]{2}|%u[0-9A-F]{3,4})*',
                                   w['name'])]
        self.assertEqual([], bad)

    @unittest.skipUnless(shutil.which('node'), 'needs node')
    def testBrowserRoundTrip(self):
        # The page shows unescape(name), and the next save sends
        # myescape(shown name). Check that gives back the stored name.
        script = """
            const vm = require('vm'), fs = require('fs');
            const ctx = {};
            vm.createContext(ctx);
            vm.runInContext(fs.readFileSync('objects.js', 'utf8'), ctx);
            const names = JSON.parse(fs.readFileSync(0, 'utf8'));
            console.log(JSON.stringify(names.map(n => ctx.myescape(unescape(n)))));
        """
        names = [w['name'] for w in self.workspaces]
        output = subprocess.run(
            ['node', '-e', script], cwd=HERE, input=json.dumps(names),
            check=True, capture_output=True, text=True).stdout
        changed = [n for n, resaved in zip(names, json.loads(output))
                   if resaved != n]
        unexpected = [n for n in changed if not UNPADDED_U.search(n)]
        self.assertEqual([], unexpected)
        print('\n%d names round-trip; %d known unpadded %%u names do not'
              % (len(names) - len(changed), len(changed)))


@unittest.skipUnless(os.path.exists(DUMP), 'needs .data/workspaces.jsonl')
class RoutingTest(unittest.TestCase):

    def setUp(self):
        self.testbed = testbed.Testbed()
        self.testbed.activate()
        self.testbed.init_datastore_v3_stub()
        self.testbed.init_memcache_stub()
        ndb.get_context().clear_cache()
        self.app = webtest.TestApp(main.app, extra_environ={
            'APPLICATION_ID': os.environ['APPLICATION_ID']})

    def tearDown(self):
        self.testbed.deactivate()

    def testClientUrlsReachTheStoredName(self):
        # The client builds workspace URLs as escape(escape(storedName)). Stored
        # names only contain [A-Za-z0-9_%], so that just escapes '%' twice.
        bad = []
        for w in load_names():
            name = w['name']
            if not name:
                # save.py accepts an empty name (one such workspace exists),
                # but no URL can reach it: /webnote/ is the home page.
                continue
            path = '/webnote/' + name.replace('%', '%2525')
            page = self.app.get(path, expect_errors=True)
            if (page.status_int != 200 or
                    "workspace.setName(unescape('%s'));" % name not in page.text):
                bad.append((name, 'page', page.status_int))
            rss = self.app.get(path + '.xml', expect_errors=True)
            if rss.status_int != 404:  # No such workspace in the testbed.
                bad.append((name, 'rss', rss.status_int))
        self.assertEqual([], bad[:20])


if __name__ == '__main__':
    unittest.main()
