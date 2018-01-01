import datetime
import json
import logging
import os
import urllib

import pytz
import jinja2
import webapp2

import constants
import models

JINJA_ENVIRONMENT = jinja2.Environment(
    loader=jinja2.FileSystemLoader(os.path.dirname(__file__)),
    extensions=['jinja2.ext.autoescape'],
    autoescape=False)

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
    """Handler for loading a workspace."""

    def get_workspace_name(self, kwargs):
        name = kwargs.get('name')
        if name:
            name = urllib.unquote(name)
        else:
            name = self.request.get('name')
        if not name:
            raise Exception()

        logging.info(name)
        return name

    def get(self, **kwargs):
        name = self.get_workspace_name(kwargs)

        next_note_num = 0
        lasttime = ''
        notes = []

        workspace = models.Workspace.get_by_wsName(name)
        if workspace:
            next_note_num = workspace.nextNoteNum
            lasttime = workspace.localtime_str()

            notes = workspace.get_notes_list(self.request)


        template_values = {
            'debugOn': constants.DEBUG,
            'name': name,
            'lasttime': lasttime,
            'HELPEMAIL': HELPEMAIL,
            'NUM_DATES': NUM_DATES,
            'nextNoteNum': next_note_num,
            'newNoteText': '',  # TODO
            'CUSTOMHEADER': CUSTOMHEADER,
            'notes': json.dumps(notes),
        }
        template = JINJA_ENVIRONMENT.get_template('templates/workspace.html')
        self.response.write(template.render(template_values))
