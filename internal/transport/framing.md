# Constants

frameHeaderLen = 4, the big-endian byte length prepended to every frame.

maxFrameBytes = 4096, the largest frame body the relay will accept.

# Vars

## ErrFrameTooLarge = "transport: frame too large"

Returned when a frame declares a length beyond maxFrameBytes.

## ErrShortWrite = "transport: short frame write"

Returned when a frame's length prefix or body was only partially written.

## ErrMalformedRecord = "transport: malformed record"

Returned when a frame is not valid JSON.

## ErrEncodeFailed = "transport: encode failed"

Returned when a record cannot be serialized.

# Functions

## readFrame(reader io.Reader) ([]byte, error)

1. Read exactly frameHeaderLen bytes and interpret them as a big-endian uint32 length.
2. if length exceeds maxFrameBytes, return ErrFrameTooLarge without reading further.
3. ReadFull a buffer of length bytes.
4. return the buffer and nil.

#### Errors

- **2.** if length exceeds maxFrameBytes, return ErrFrameTooLarge.

## writeFrame(writer io.Writer, data []byte) error

1. Write the big-endian uint32 of len(data).
2. Write data and compare the byte count to len(data).
3. if fewer bytes were written, return ErrShortWrite.
4. return nil.

#### Errors

- **3.** if fewer bytes were written, return ErrShortWrite.

## readJSON(reader io.Reader, target interface{}) error

1. Read one frame with readFrame.
2. Unmarshal the frame into target.
3. if unmarshal fails, return ErrMalformedRecord.
4. return nil.

#### Errors

- **3.** if unmarshal fails, return ErrMalformedRecord.

## writeJSON(writer io.Writer, value interface{}) error

1. Marshal value.
2. if marshal fails, return ErrEncodeFailed.
3. writeFrame with the marshaled bytes.
4. return the write error, if any.

#### Errors

- **2.** if marshal fails, return ErrEncodeFailed.
