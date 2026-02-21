from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable
from typing import ClassVar as _ClassVar, Optional as _Optional

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
