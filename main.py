# Copyright 2016 Google Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import datetime
import json
import logging
import os
import urllib

import jinja2
import pytz
import webapp2

import models
import getdates_handler
import getrecent_handler
import save_handler

JINJA_ENVIRONMENT = jinja2.Environment(
		loader=jinja2.FileSystemLoader(os.path.dirname(__file__)),
		extensions=['jinja2.ext.autoescape'],
		autoescape=False)

DEBUG = 0
HELPEMAIL = 'webnote@ponderer.org'
NUM_DATES = 10
CUSTOMHEADER = """
<script>
(function(i,s,o,g,r,a,m){i['GoogleAnalyticsObject']=r;i[r]=i[r]||function(){
(i[r].q=i[r].q||[]).push(arguments)},i[r].l=1*new Date();a=s.createElement(o),
m=s.getElementsByTagName(o)[0];a.async=1;a.src=g;m.parentNode.insertBefore(a,m)
})(window,document,'script','//www.google-analytics.com/analytics.js','ga');

ga('create', 'UA-105084-2', 'auto');
ga('send', 'pageview');
</script>
"""


class LoadHandler(webapp2.RequestHandler):
    def get(self, **kwargs):
        name = kwargs.get('name')
        if name:
            name = urllib.unquote(name)
        else:
            name = self.request.get('name')
        if not name:
            raise Exception()

        nextNoteNum = 0
        lasttime = ''
        notes = []
        logging.info(name)

        workspace = models.Workspace.query(models.Workspace.name==name).order(
            -models.Workspace.time).get()
        if workspace:
            nextNoteNum = workspace.nextNoteNum
            lasttime = workspace.localtime_str()

            dt = workspace.time
            loadTime = self.request.get('time')
            if loadTime:
                try:
                    dt = datetime.datetime.strptime(loadTime, '%Y-%m-%d %H:%M:%S')
                    dt = models.TIMEZONE.localize(dt).astimezone(pytz.utc)
                except ValueError:
                    pass
            
            notesEntity = models.Notes.query(
            		models.Notes.workspaceKey==workspace.key and
            		models.Notes.time==dt).get()

            if notesEntity:
                notes = notesEntity.notesJsonArray

                
        template_values = {
            'debugOn': DEBUG,
            'name': name,
            'lasttime': lasttime,
            'HELPEMAIL': HELPEMAIL,
            'NUM_DATES': NUM_DATES,
            'nextNoteNum': nextNoteNum,
            'newNoteText': '',  # TODO
            'CUSTOMHEADER': CUSTOMHEADER,
            'notes': json.dumps(notes),
        }
        template = JINJA_ENVIRONMENT.get_template('templates/workspace.html')
        self.response.write(template.render(template_values))


class StringHandler(webapp2.RequestHandler):

    CACHE = {}

    def get(self):
        self.response.headers['Content-Type'] = 'text/javascript'

        lang = 'en'

        if lang not in self.CACHE:
            path = os.path.join(os.path.split(__file__)[0], 'strings.js.' + lang)
            with open(path) as f:
                self.CACHE[lang] = f.read()

        self.response.write(self.CACHE[lang])


app = webapp2.WSGIApplication([
    webapp2.Route('/', webapp2.RedirectHandler, defaults={'_uri': '/webnote/'}),
    webapp2.Route('/webnote', webapp2.RedirectHandler, defaults={'_uri': '/webnote/'}),
    webapp2.Route('/webnote/index.html', webapp2.RedirectHandler, defaults={'_uri': '/webnote/'}),

    webapp2.Route(r'/webnote/strings.js', handler=StringHandler),
    webapp2.Route(r'/webnote/save.py', handler=save_handler.SaveHandler),
    webapp2.Route(r'/webnote/getrecent.py', handler=getrecent_handler.GetRecentHandler),
    webapp2.Route(r'/webnote/getdates.py', handler=getdates_handler.GetDatesHandler),
    webapp2.Route(r'/webnote/load.py', handler=LoadHandler),
    webapp2.Route(r'/webnote/<name:.+>', handler=LoadHandler),
], debug=DEBUG)
