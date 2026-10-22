import json
import logging
import os
import urllib.parse

import jinja2
import webapp2

import constants
import models

JINJA_ENVIRONMENT = jinja2.Environment(
    loader=jinja2.FileSystemLoader(os.path.dirname(__file__)),
    autoescape=False)

HELPEMAIL = 'webnote@ponderer.org'
NUM_DATES = 10

class LoadHandler(webapp2.RequestHandler):
    """Handler for loading a workspace."""

    def get_workspace_name(self, kwargs):
        name = kwargs.get('name')
        if name:
            name = urllib.parse.unquote(name)
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
            'notes': json.dumps(notes),
        }
        template = JINJA_ENVIRONMENT.get_template('templates/workspace.html')
        self.response.write(template.render(template_values))
