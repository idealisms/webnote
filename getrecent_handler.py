import datetime

from google.appengine.ext import ndb
import pytz
import webapp2

import models

class GetRecentHandler(webapp2.RequestHandler):
    def get(self, **kwargs):
      self.response.headers['Content-Type'] = 'text/plain'
      name = self.request.get('name', '')
      if not name:
          return
      workspace = models.Workspace.query(models.Workspace.name==name).get()
      if not workspace:
          return
      self.response.write(workspace.localtime_str())
