Wire JSON is snake_case: Hello.Role, Hello.Token, Response.Status, Response.SessionID, Response.ErrorCode. Serialization travels through framing.readJSON/writeJSON; this file defines only the record shapes and their validation.

# Constants

helloVersion = 1, the only protocol version accepted by both peers.

statusCodeOK = "ok", the relay verdict that authentication succeeded.

statusCodeError = "error", the relay verdict that authentication failed.

# Vars

## ErrProtocolVersion = "transport: unexpected protocol version"

Returned when a record advertises a version other than helloVersion.

## ErrUnexpectedRole = "transport: unexpected role"

Returned when Hello.Role is neither exit nor proxy.

## ErrUnexpectedStatus = "transport: unexpected status"

Returned when Response.Status is neither statusCodeOK nor statusCodeError.

# Types

## Hello

1. Version uint16
2. Role Mode — exit or proxy.
3. Token string — bearer presented to the relay.

Sent by a client. Role selects the relay's expected bearer.

## Response

1. Version uint16
2. Status StatusCode — statusCodeOK or statusCodeError.
3. SessionID string — opaque handle returned on success.
4. ErrorCode string — bounded reason returned on failure.

Sent by the relay. SessionID is non-secret; ErrorCode is drawn from a small fixed vocabulary, never free-form text.

## StatusCode string

The relay verdict. Values are statusCodeOK and statusCodeError.

# Functions

## validateHello(hello Hello) error

1. if Version differs from helloVersion, return ErrProtocolVersion.
2. if Role is neither exit nor proxy, return ErrUnexpectedRole.
3. return nil.

#### Errors

- **1.** if Version differs from helloVersion, return ErrProtocolVersion.
- **2.** if Role is neither exit nor proxy, return ErrUnexpectedRole.

## validateResponse(resp Response) error

1. if Version differs from helloVersion, return ErrProtocolVersion.
2. if Status is neither statusCodeOK nor statusCodeError, return ErrUnexpectedStatus.
3. return nil.

#### Errors

- **1.** if Version differs from helloVersion, return ErrProtocolVersion.
- **2.** if Status is neither statusCodeOK nor statusCodeError, return ErrUnexpectedStatus.

## newSessionID() string

1. Read 16 bytes from crypto/rand.
2. Hex-encode them.
3. return the id.
