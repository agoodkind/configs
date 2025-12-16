def as_time_ms($ms):
  if ($ms | type) == "number" then
    (($ms / 1000) | todateiso8601)
  else
    "-"
  end;

def empty_obj:
  (. == null) or (. == {});

def data_str($e):
  if (($e.data // {}) | empty_obj) then
    "{}"
  else
    ($e.data | tojson)
  end;

def fmt($e):
  "\((as_time_ms($e.timestamp))) \($e.traceId // "-") \($e.component // "-") \($e.location // "-") \($e.message // "-") " +
  (data_str($e)) +
  (if ($e.dataParseError == true) then " dataParseError=true" else "" end);

fromjson? as $e
| select($e != null)
| select(($id == "") or (($e.traceId // "") == $id))
| fmt($e)
