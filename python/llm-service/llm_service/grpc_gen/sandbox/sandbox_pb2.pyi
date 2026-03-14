from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Iterable as _Iterable, Mapping as _Mapping, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class FileReadRequest(_message.Message):
    __slots__ = ("session_id", "path", "max_bytes", "encoding", "user_id")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    MAX_BYTES_FIELD_NUMBER: _ClassVar[int]
    ENCODING_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    path: str
    max_bytes: int
    encoding: str
    user_id: str
    def __init__(self, session_id: _Optional[str] = ..., path: _Optional[str] = ..., max_bytes: _Optional[int] = ..., encoding: _Optional[str] = ..., user_id: _Optional[str] = ...) -> None: ...

class FileReadResponse(_message.Message):
    __slots__ = ("success", "content", "error", "size_bytes", "file_type")
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    SIZE_BYTES_FIELD_NUMBER: _ClassVar[int]
    FILE_TYPE_FIELD_NUMBER: _ClassVar[int]
    success: bool
    content: str
    error: str
    size_bytes: int
    file_type: str
    def __init__(self, success: bool = ..., content: _Optional[str] = ..., error: _Optional[str] = ..., size_bytes: _Optional[int] = ..., file_type: _Optional[str] = ...) -> None: ...

class FileWriteRequest(_message.Message):
    __slots__ = ("session_id", "path", "content", "append", "create_dirs", "encoding", "user_id")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    APPEND_FIELD_NUMBER: _ClassVar[int]
    CREATE_DIRS_FIELD_NUMBER: _ClassVar[int]
    ENCODING_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    path: str
    content: str
    append: bool
    create_dirs: bool
    encoding: str
    user_id: str
    def __init__(self, session_id: _Optional[str] = ..., path: _Optional[str] = ..., content: _Optional[str] = ..., append: bool = ..., create_dirs: bool = ..., encoding: _Optional[str] = ..., user_id: _Optional[str] = ...) -> None: ...

class FileWriteResponse(_message.Message):
    __slots__ = ("success", "bytes_written", "error", "absolute_path")
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    BYTES_WRITTEN_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    ABSOLUTE_PATH_FIELD_NUMBER: _ClassVar[int]
    success: bool
    bytes_written: int
    error: str
    absolute_path: str
    def __init__(self, success: bool = ..., bytes_written: _Optional[int] = ..., error: _Optional[str] = ..., absolute_path: _Optional[str] = ...) -> None: ...

class FileListRequest(_message.Message):
    __slots__ = ("session_id", "path", "pattern", "recursive", "include_hidden", "user_id")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    PATTERN_FIELD_NUMBER: _ClassVar[int]
    RECURSIVE_FIELD_NUMBER: _ClassVar[int]
    INCLUDE_HIDDEN_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    path: str
    pattern: str
    recursive: bool
    include_hidden: bool
    user_id: str
    def __init__(self, session_id: _Optional[str] = ..., path: _Optional[str] = ..., pattern: _Optional[str] = ..., recursive: bool = ..., include_hidden: bool = ..., user_id: _Optional[str] = ...) -> None: ...

class FileEntry(_message.Message):
    __slots__ = ("name", "path", "is_file", "size_bytes", "modified_time")
    NAME_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    IS_FILE_FIELD_NUMBER: _ClassVar[int]
    SIZE_BYTES_FIELD_NUMBER: _ClassVar[int]
    MODIFIED_TIME_FIELD_NUMBER: _ClassVar[int]
    name: str
    path: str
    is_file: bool
    size_bytes: int
    modified_time: int
    def __init__(self, name: _Optional[str] = ..., path: _Optional[str] = ..., is_file: bool = ..., size_bytes: _Optional[int] = ..., modified_time: _Optional[int] = ...) -> None: ...

class FileListResponse(_message.Message):
    __slots__ = ("success", "entries", "error", "file_count", "dir_count")
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    ENTRIES_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    FILE_COUNT_FIELD_NUMBER: _ClassVar[int]
    DIR_COUNT_FIELD_NUMBER: _ClassVar[int]
    success: bool
    entries: _containers.RepeatedCompositeFieldContainer[FileEntry]
    error: str
    file_count: int
    dir_count: int
    def __init__(self, success: bool = ..., entries: _Optional[_Iterable[_Union[FileEntry, _Mapping]]] = ..., error: _Optional[str] = ..., file_count: _Optional[int] = ..., dir_count: _Optional[int] = ...) -> None: ...

class FileSearchRequest(_message.Message):
    __slots__ = ("session_id", "query", "path", "max_results", "regex", "include", "context_lines")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    QUERY_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    MAX_RESULTS_FIELD_NUMBER: _ClassVar[int]
    REGEX_FIELD_NUMBER: _ClassVar[int]
    INCLUDE_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_LINES_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    query: str
    path: str
    max_results: int
    regex: bool
    include: str
    context_lines: int
    def __init__(self, session_id: _Optional[str] = ..., query: _Optional[str] = ..., path: _Optional[str] = ..., max_results: _Optional[int] = ..., regex: bool = ..., include: _Optional[str] = ..., context_lines: _Optional[int] = ...) -> None: ...

class SearchMatch(_message.Message):
    __slots__ = ("file", "line", "content", "context")
    FILE_FIELD_NUMBER: _ClassVar[int]
    LINE_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_FIELD_NUMBER: _ClassVar[int]
    file: str
    line: int
    content: str
    context: _containers.RepeatedCompositeFieldContainer[ContextLine]
    def __init__(self, file: _Optional[str] = ..., line: _Optional[int] = ..., content: _Optional[str] = ..., context: _Optional[_Iterable[_Union[ContextLine, _Mapping]]] = ...) -> None: ...

class ContextLine(_message.Message):
    __slots__ = ("line", "content")
    LINE_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    line: int
    content: str
    def __init__(self, line: _Optional[int] = ..., content: _Optional[str] = ...) -> None: ...

class FileSearchResponse(_message.Message):
    __slots__ = ("success", "matches", "error", "files_scanned", "matches_found", "truncated")
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    MATCHES_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    FILES_SCANNED_FIELD_NUMBER: _ClassVar[int]
    MATCHES_FOUND_FIELD_NUMBER: _ClassVar[int]
    TRUNCATED_FIELD_NUMBER: _ClassVar[int]
    success: bool
    matches: _containers.RepeatedCompositeFieldContainer[SearchMatch]
    error: str
    files_scanned: int
    matches_found: int
    truncated: bool
    def __init__(self, success: bool = ..., matches: _Optional[_Iterable[_Union[SearchMatch, _Mapping]]] = ..., error: _Optional[str] = ..., files_scanned: _Optional[int] = ..., matches_found: _Optional[int] = ..., truncated: bool = ...) -> None: ...

class FileEditRequest(_message.Message):
    __slots__ = ("session_id", "path", "old_text", "new_text", "replace_all")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    OLD_TEXT_FIELD_NUMBER: _ClassVar[int]
    NEW_TEXT_FIELD_NUMBER: _ClassVar[int]
    REPLACE_ALL_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    path: str
    old_text: str
    new_text: str
    replace_all: bool
    def __init__(self, session_id: _Optional[str] = ..., path: _Optional[str] = ..., old_text: _Optional[str] = ..., new_text: _Optional[str] = ..., replace_all: bool = ...) -> None: ...

class FileEditResponse(_message.Message):
    __slots__ = ("success", "error", "replacements", "snippet", "file_size_after")
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    REPLACEMENTS_FIELD_NUMBER: _ClassVar[int]
    SNIPPET_FIELD_NUMBER: _ClassVar[int]
    FILE_SIZE_AFTER_FIELD_NUMBER: _ClassVar[int]
    success: bool
    error: str
    replacements: int
    snippet: str
    file_size_after: int
    def __init__(self, success: bool = ..., error: _Optional[str] = ..., replacements: _Optional[int] = ..., snippet: _Optional[str] = ..., file_size_after: _Optional[int] = ...) -> None: ...

class CommandRequest(_message.Message):
    __slots__ = ("session_id", "command", "timeout_seconds", "user_id")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    COMMAND_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_SECONDS_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    command: str
    timeout_seconds: int
    user_id: str
    def __init__(self, session_id: _Optional[str] = ..., command: _Optional[str] = ..., timeout_seconds: _Optional[int] = ..., user_id: _Optional[str] = ...) -> None: ...

class CommandResponse(_message.Message):
    __slots__ = ("success", "stdout", "stderr", "exit_code", "error", "execution_time_ms")
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    STDOUT_FIELD_NUMBER: _ClassVar[int]
    STDERR_FIELD_NUMBER: _ClassVar[int]
    EXIT_CODE_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_TIME_MS_FIELD_NUMBER: _ClassVar[int]
    success: bool
    stdout: str
    stderr: str
    exit_code: int
    error: str
    execution_time_ms: int
    def __init__(self, success: bool = ..., stdout: _Optional[str] = ..., stderr: _Optional[str] = ..., exit_code: _Optional[int] = ..., error: _Optional[str] = ..., execution_time_ms: _Optional[int] = ...) -> None: ...

class FileDeleteRequest(_message.Message):
    __slots__ = ("session_id", "path", "pattern", "recursive")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    PATTERN_FIELD_NUMBER: _ClassVar[int]
    RECURSIVE_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    path: str
    pattern: str
    recursive: bool
    def __init__(self, session_id: _Optional[str] = ..., path: _Optional[str] = ..., pattern: _Optional[str] = ..., recursive: bool = ...) -> None: ...

class FileDeleteResponse(_message.Message):
    __slots__ = ("success", "error", "deleted_count", "deleted_paths")
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    DELETED_COUNT_FIELD_NUMBER: _ClassVar[int]
    DELETED_PATHS_FIELD_NUMBER: _ClassVar[int]
    success: bool
    error: str
    deleted_count: int
    deleted_paths: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, success: bool = ..., error: _Optional[str] = ..., deleted_count: _Optional[int] = ..., deleted_paths: _Optional[_Iterable[str]] = ...) -> None: ...
