"""Request models from the pinned Obstor SDK's current public model API."""

from obstor.models import SelectObjectContentRequest

SelectRequest = SelectObjectContentRequest
CSVInputSerialization = SelectRequest.CSVInputSerialization
CSVOutputSerialization = SelectRequest.CSVOutputSerialization
JSONInputSerialization = SelectRequest.JSONInputSerialization
JSONOutputSerialization = SelectRequest.JSONOutputSerialization

# SDK serializes these fields with str(), so supply the protocol value
COMPRESSION_TYPE_NONE = SelectRequest.CompressionType.NONE.value
FILE_HEADER_INFO_NONE = SelectRequest.FileHeaderInfo.NONE.value
JSON_TYPE_DOCUMENT = SelectRequest.JsonType.DOCUMENT.value
QUOTE_FIELDS_ALWAYS = SelectRequest.QuoteFields.ALWAYS.value
QUOTE_FIELDS_ASNEEDED = SelectRequest.QuoteFields.ASNEEDED.value
