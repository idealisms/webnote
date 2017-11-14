import datetime

from google.appengine.ext import ndb
import pytz
import webapp2

import models

class GetDatesHandler(webapp2.RequestHandler):
    def get(self, **kwargs):
      self.response.headers['Content-Type'] = 'text/plain'
      name = self.request.get('name', '')
      offset = self.request.get('offset', '0')
      if not name:
          return
      workspace = models.Workspace.get_by_wsName(name)
      if not workspace:
          return

      try:
          offset = int(offset)
      except:
          offset = 0

      notes = models.Notes.query(models.Notes.workspaceKey==workspace.key).order(
          -models.Notes.time).fetch(offset=offset, limit=11)

      self.response.write('|'.join(n.localtime_str() for n in notes))
