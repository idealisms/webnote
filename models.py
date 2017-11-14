import datetime
import hashlib

import pytz
from google.appengine.ext import ndb

TIMEZONE = pytz.timezone('US/Eastern')

class Workspace(ndb.Model):
    name = ndb.StringProperty()
    nextNoteNum = ndb.IntegerProperty(required=True)
    time = ndb.DateTimeProperty(required=True)

    @staticmethod
    def get_by_wsName(wsName):
        key_id = hashlib.sha1(wsName).hexdigest()
        return Workspace.get_by_id(key_id)

    @staticmethod
    def create(wsName, nextNoteNum, time):
        key_id = hashlib.sha1(wsName).hexdigest()

        return Workspace(id=key_id, name=wsName, nextNoteNum=nextNoteNum,
                         time=time)

    def localtime_str(self):
        local_datetime = pytz.utc.localize(self.time).astimezone(TIMEZONE)
        return local_datetime.strftime('%Y-%m-%d %H:%M:%S')


class Notes(ndb.Model):

    JSKEYS = ('id', 'xPos', 'yPos', 'height', 'width',
              'bgcolor', 'zIndex', 'text')
    DBKEYS = ('noteid', 'xposition', 'yposition', 'height', 'width',
              'bgcolor', 'zindex', 'text')
    DB2JS = dict(zip(DBKEYS, JSKEYS))

    workspaceKey = ndb.KeyProperty(kind='Workspace', required=True)
    time = ndb.DateTimeProperty(required=True)
    notesJsonArray = ndb.JsonProperty(compressed=True, required=True)

    def localtime_str(self):
        local_datetime = pytz.utc.localize(self.time).astimezone(TIMEZONE)
        return local_datetime.strftime('%Y-%m-%d %H:%M:%S')
