from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class GenerateRequest(_message.Message):
    __slots__ = ("prompt", "state_vector", "evidence")
    PROMPT_FIELD_NUMBER: _ClassVar[int]
    STATE_VECTOR_FIELD_NUMBER: _ClassVar[int]
    EVIDENCE_FIELD_NUMBER: _ClassVar[int]
    prompt: str
    state_vector: _containers.RepeatedScalarFieldContainer[float]
    evidence: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, prompt: _Optional[str] = ..., state_vector: _Optional[_Iterable[float]] = ..., evidence: _Optional[_Iterable[str]] = ...) -> None: ...

class GenerateResponse(_message.Message):
    __slots__ = ("text", "entropy", "logits")
    TEXT_FIELD_NUMBER: _ClassVar[int]
    ENTROPY_FIELD_NUMBER: _ClassVar[int]
    LOGITS_FIELD_NUMBER: _ClassVar[int]
    text: str
    entropy: float
    logits: _containers.RepeatedScalarFieldContainer[float]
    def __init__(self, text: _Optional[str] = ..., entropy: _Optional[float] = ..., logits: _Optional[_Iterable[float]] = ...) -> None: ...

class EmbedRequest(_message.Message):
    __slots__ = ("text",)
    TEXT_FIELD_NUMBER: _ClassVar[int]
    text: str
    def __init__(self, text: _Optional[str] = ...) -> None: ...

class EmbedResponse(_message.Message):
    __slots__ = ("embedding",)
    EMBEDDING_FIELD_NUMBER: _ClassVar[int]
    embedding: _containers.RepeatedScalarFieldContainer[float]
    def __init__(self, embedding: _Optional[_Iterable[float]] = ...) -> None: ...

class SearchRequest(_message.Message):
    __slots__ = ("query_text", "query_embedding", "top_k", "similarity_threshold")
    QUERY_TEXT_FIELD_NUMBER: _ClassVar[int]
    QUERY_EMBEDDING_FIELD_NUMBER: _ClassVar[int]
    TOP_K_FIELD_NUMBER: _ClassVar[int]
    SIMILARITY_THRESHOLD_FIELD_NUMBER: _ClassVar[int]
    query_text: str
    query_embedding: _containers.RepeatedScalarFieldContainer[float]
    top_k: int
    similarity_threshold: float
    def __init__(self, query_text: _Optional[str] = ..., query_embedding: _Optional[_Iterable[float]] = ..., top_k: _Optional[int] = ..., similarity_threshold: _Optional[float] = ...) -> None: ...

class SearchResult(_message.Message):
    __slots__ = ("id", "text", "score", "metadata_json")
    ID_FIELD_NUMBER: _ClassVar[int]
    TEXT_FIELD_NUMBER: _ClassVar[int]
    SCORE_FIELD_NUMBER: _ClassVar[int]
    METADATA_JSON_FIELD_NUMBER: _ClassVar[int]
    id: str
    text: str
    score: float
    metadata_json: str
    def __init__(self, id: _Optional[str] = ..., text: _Optional[str] = ..., score: _Optional[float] = ..., metadata_json: _Optional[str] = ...) -> None: ...

class SearchResponse(_message.Message):
    __slots__ = ("results",)
    RESULTS_FIELD_NUMBER: _ClassVar[int]
    results: _containers.RepeatedCompositeFieldContainer[SearchResult]
    def __init__(self, results: _Optional[_Iterable[_Union[SearchResult, _Mapping]]] = ...) -> None: ...

class StoreEvidenceRequest(_message.Message):
    __slots__ = ("text", "metadata_json")
    TEXT_FIELD_NUMBER: _ClassVar[int]
    METADATA_JSON_FIELD_NUMBER: _ClassVar[int]
    text: str
    metadata_json: str
    def __init__(self, text: _Optional[str] = ..., metadata_json: _Optional[str] = ...) -> None: ...

class StoreEvidenceResponse(_message.Message):
    __slots__ = ("id",)
    ID_FIELD_NUMBER: _ClassVar[int]
    id: str
    def __init__(self, id: _Optional[str] = ...) -> None: ...
