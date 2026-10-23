import datetime
import hashlib
import json
import logging
import re
import string
import zlib

import pytz
from google.appengine.ext import ndb

TIMEZONE = pytz.timezone('US/Eastern')

# Characters that myescape() in objects.js leaves unescaped.
_MYESCAPE_SAFE = frozenset(string.ascii_letters + string.digits + '_')


def _myescape_char(c):
    """Escapes one character the way myescape() in objects.js does.

    Like JavaScript, this works on UTF-16 code units, so characters outside the
    BMP become two escapes (a surrogate pair).
    """
    units = c.encode('utf-16-be', 'surrogatepass')
    ret = ''
    for i in range(0, len(units), 2):
        hex_str = '%02X' % int.from_bytes(units[i:i + 2], 'big')
        if len(hex_str) > 2:
            hex_str = 'u' + hex_str
        ret += '%' + hex_str
    return ret


def canonical_ws_name(name):
    """Returns the stored form of a workspace name.

    The client escapes workspace names itself, and the stored name is the escaped
    form. Browsers used to escape differently: Chrome, Safari and Edge used
    myescape() from objects.js, while Firefox used the native escape(). This maps
    any of those forms (or an unescaped name) to the myescape() form:
    - Native escape() leaves @*+-./ unescaped; myescape() escapes them
      (e.g. '-' -> '%2D').
    - Native escape() pads %u escapes to 4 hex digits; myescape() doesn't
      (e.g. '%u05D9' -> '%u5D9').
    - Any other character myescape() would escape (e.g. a raw 'á' from a URL
      typed by hand) is escaped ('á' -> '%E1').
    '%' always starts an escape sequence, since both functions escape a literal
    '%' as '%25'.
    """
    escaped = ''.join(
        c if c in _MYESCAPE_SAFE or c == '%' else _myescape_char(c)
        for c in name)
    return re.sub(r'%u0([0-9A-Fa-f]{3})', r'%u\1', escaped)


class Workspace(ndb.Model):
    # The Go version writes to Datastore directly, which would leave ndb's
    # memcache copies stale if Python serves again (a rollback).
    _use_memcache = False

    name = ndb.StringProperty()
    nextNoteNum = ndb.IntegerProperty(required=True)
    time = ndb.DateTimeProperty(required=True)

    @staticmethod
    def key_id(wsName):
        return hashlib.sha1(
            canonical_ws_name(wsName).encode('ascii')).hexdigest()

    @staticmethod
    def get_by_wsName(wsName):
        return Workspace.get_by_id(Workspace.key_id(wsName))

    @staticmethod
    def create(wsName, nextNoteNum, time):
        return Workspace(id=Workspace.key_id(wsName),
                         name=canonical_ws_name(wsName),
                         nextNoteNum=nextNoteNum, time=time)

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
                Notes.workspaceKey == self.key,
                Notes.time == dt).get()

        ret = []
        if notes_entity:
            # Legacy entities store notesJsonArray as a plain zlib blob that
            # the ndb property can't decode, so it loads as raw bytes. Entities
            # written by ndb load as a _CompressedValue and use the property.
            v = notes_entity._values.get(b'notesJsonArray')
            raw = getattr(v, 'b_val', None)
            if isinstance(raw, bytes):
                try:
                    return json.loads(zlib.decompress(raw).decode('utf8'))
                except (zlib.error, ValueError):
                    logging.exception('Failed to decode legacy notesJsonArray')

            try:
                ret = notes_entity.notesJsonArray
            except UnicodeDecodeError:
                logging.exception('Failed to decode notesJsonArray')

        return ret


class Notes(ndb.Model):
    _use_memcache = False

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
