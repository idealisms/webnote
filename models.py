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
        key_id = hashlib.sha1(wsName.encode('ascii')).hexdigest()
        return Workspace.get_by_id(key_id)

    @staticmethod
    def create(wsName, nextNoteNum, time):
        key_id = hashlib.sha1(wsName.encode('ascii')).hexdigest()

        return Workspace(id=key_id, name=wsName, nextNoteNum=nextNoteNum,
                         time=time)

    def localtime_str(self):
        local_datetime = pytz.utc.localize(self.time).astimezone(TIMEZONE)
        return local_datetime.strftime('%Y-%m-%d %H:%M:%S')

    def get_notes_list(self, request):
        dt = self.time
        load_time = request and request.get('time')
        if load_time:
            try:
                dt = datetime.datetime.strptime(load_time, '%Y-%m-%d %H:%M:%S')
                dt = TIMEZONE.localize(dt).astimezone(pytz.utc)
            except ValueError:
                pass

        notes_entity = Notes.query(
                Notes.workspaceKey == self.key and
                Notes.time == dt).get()

        if notes_entity:
            return notes_entity.notesJsonArray
        return []
    



class Notes(ndb.Model):

    JSKEYS = ('id', 'xPos', 'yPos', 'height', 'width',
              'bgcolor', 'zIndex', 'text')
    DBKEYS = ('noteid', 'xposition', 'yposition', 'height', 'width',
              'bgcolor', 'zindex', 'text')
    DB2JS = dict(zip(DBKEYS, JSKEYS))

    workspaceKey = ndb.KeyProperty(kind='Workspace', required=True)
    time = ndb.DateTimeProperty(required=True)
    notesJsonArray = ndb.JsonProperty(compressed=True, required=True)
    time_to_delete = ndb.DateTimeProperty()

    def localtime_str(self):
        local_datetime = pytz.utc.localize(self.time).astimezone(TIMEZONE)
        return local_datetime.strftime('%Y-%m-%d %H:%M:%S')
