"""Writes golden files for the Go port's tests, using the Python code.

Usage (from the repo root):
  env/bin/python tools/gen_golden.py

The Go tests read testdata/*.json, so they check against what Python actually
does rather than a reading of it. Rerun this when the Python behavior changes,
and commit the output.
"""

import datetime
import json
import os
import sys

import pytz

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
import models  # noqa: E402

TESTDATA = os.path.join(
    os.path.dirname(os.path.dirname(os.path.abspath(__file__))), 'testdata')


def parse_load_time(value):
    """Parses the load page's time parameter like Workspace.get_notes_list().

    Returns 'fallback' when get_notes_list() falls back to the latest revision
    (ValueError), 'error' when it raises (a 500), or the UTC time it queries.
    """
    # Same steps as get_notes_list(), which needs a datastore to call directly.
    try:
        dt = datetime.datetime.strptime(value, '%Y-%m-%d %H:%M:%S')
        dt = models.TIMEZONE.localize(dt).astimezone(pytz.utc)
    except ValueError:
        return 'fallback'
    except Exception:
        return 'error'
    return format_time(dt)


def format_time(dt):
    # strftime('%Y') doesn't pad years before 1000.
    return '%04d-%02d-%02d %02d:%02d:%02d' % (
        dt.year, dt.month, dt.day, dt.hour, dt.minute, dt.second)


def load_time_cases():
    values = [
        '2017-12-30 12:00:00',
        '2017-07-04 12:30:15',
        # strptime accepts unpadded and space-padded fields.
        '2017-1-5 1:2:3',
        '2017-01- 5 01:02:03',
        '2017-01-5 01:02:03',
        # Field ranges and date validity.
        '2017-13-01 00:00:00',
        '2017-00-01 00:00:00',
        '2017-02-29 00:00:00',
        '2016-02-29 00:00:00',
        '2017-04-31 00:00:00',
        '2017-12-31 24:00:00',
        '2017-12-31 23:60:00',
        '2017-12-31 23:59:60',
        '2017-12-31 23:59:61',
        '2017-12-31 23:59:62',
        '0000-01-01 00:00:00',
        '0001-01-01 00:00:00',
        '0001-01-01 23:59:59',
        '0001-01-02 00:00:00',
        '9999-12-30 23:59:59',
        '9999-12-31 00:00:00',
        '9999-12-31 18:59:59',
        '9999-12-31 19:00:00',
        '9999-12-31 23:59:59',
        # Not the format.
        '',
        'not a time',
        '2017-12-30',
        '2017-12-30 12:00',
        '2017-12-30 12:00:00 ',
        ' 2017-12-30 12:00:00',
        '2017-12-30T12:00:00',
        '2017/12/30 12:00:00',
        '17-12-30 12:00:00',
        '20171-12-30 12:00:00',
        '2017-12-30 12:00:000',
        '2017-012-30 12:00:00',
        '+2017-12-30 12:00:00',
        '-2017-12-30 12:00:00',
        '2017-12-30 12:00:00.5',
        # The separator between date and time is \s+.
        '2017-12-30  12:00:00',
        '2017-12-30\t12:00:00',
        '2017-12-30\n12:00:00',
        '2017-12-30 \t 12:00:00',
        '2017-12-3012:00:00',
    ]
    # Characters accepted as whitespace (all of them) or as digits (ASCII
    # only: the Go port doesn't accept other scripts' digits, which depend on
    # the Unicode version).
    for i in range(0x110000):
        c = chr(i)
        if i < 128 or c.isspace():
            values.append('2017-12-30' + c + '12:00:00')
        if i < 128:
            values.append('2017-12-30 12:00:0' + c)
    # Every half hour around each DST transition, plus ordinary times.
    tz = models.TIMEZONE
    for year in range(2000, 2031):
        for month in (3, 11):
            for day in range(1, 15):
                for minute in range(0, 5 * 60, 30):
                    values.append('%04d-%02d-%02d %02d:%02d:00' % (
                        year, month, day, minute // 60, minute % 60))
            values.append('%04d-%02d-%02d 01:59:59' % (year, month, 8))
    for year in range(1880, 1975, 7):
        values.append('%04d-06-15 12:00:00' % year)
        values.append('%04d-01-15 12:00:00' % year)
    seen = set()
    cases = []
    for value in values:
        if value not in seen:
            seen.add(value)
            cases.append([value, parse_load_time(value)])
    return cases


def localtime_cases():
    """UTC times and their Workspace.localtime_str()."""
    cases = []
    times = []
    for year in range(2000, 2031):
        for month in (3, 11):
            for day in range(1, 15):
                start = datetime.datetime(year, month, day, 4, 0, 0)
                for minutes in range(0, 5 * 60, 30):
                    times.append(start + datetime.timedelta(minutes=minutes))
    times.append(datetime.datetime(1, 1, 1, 12, 0, 0))
    times.append(datetime.datetime(1900, 1, 1, 0, 0, 0))
    times.append(datetime.datetime(9999, 12, 31, 23, 59, 59))
    for time in times:
        workspace = models.Workspace.create('a', 0, time)
        cases.append([format_time(time),
                      workspace.localtime_str()])
    return cases


def write(name, value):
    path = os.path.join(TESTDATA, name)
    with open(path, 'w') as f:
        json.dump(value, f, indent=0, ensure_ascii=True)
        f.write('\n')
    print('wrote', path)


def main():
    os.makedirs(TESTDATA, exist_ok=True)
    write('load_time.json', load_time_cases())
    write('localtime.json', localtime_cases())


if __name__ == '__main__':
    main()
