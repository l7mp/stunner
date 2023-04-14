# Authentication

STUNner uses the IETF STUN/TURN protocol suite to ingest media traffic into the Kubernetes cluster,
which, [by design](https://datatracker.ietf.org/doc/html/rfc5766#section-17), provides
comprehensive security. In particular, STUNner provides message integrity and, if configured with
the TLS/TCP or DTLS/UDP listeners, complete confidentiality. To complete the CIA triad, this guide
shows how to configure user authentication with STUNner.

## The long-term credential mechanism

STUNner relies on the STUN [long-term credential
mechanism](https://www.rfc-editor.org/rfc/rfc8489.html#page-26) to provide user authentication.

The long-term credential mechanism assumes that, prior to the communication, STUNner and the WebRTC
clients agree on a username and password to be used for authentication.  The credential is
considered long-term since it is assumed that it is provisioned for a user and remains in effect
until the user is no longer a subscriber of the system (STUNner's `static` authentication mode),
or until the predefined lifetime of the username/password pair passes and the credential expires
(`ephemeral` authentication mode in STUNner).

STUNner secures the authentication process against replay attacks using a digest challenge.  In
this mechanism, the server sends the user a realm (used to guide the user or agent in selection of
a username and password) and a nonce.  The nonce provides replay protection.  The client also
includes a message-integrity attribute in the authentication message, which provides an HMAC over
the entire request, including the nonce.  The server validates the nonce and checks the message
integrity.  If they match, the request is authenticated, otherwise the server rejects the request.

## Authentication workflow

The intended authentication workflow in STUNner is as follows.

1. *A username/password pair is generated.* This is outside the scope of STUNner; however, STUNner
   comes with a comprehensive [authentication
   service](https://github.com/l7mp/stunner-auth-service) that can be queried for a valid ICE
   configuration for STUNner.  The ICE configs returned by this service can be directly
   [supplied](https://developer.mozilla.org/en-US/docs/Web/API/RTCPeerConnection/RTCPeerConnection#parameters)
   by clients to the `PeerConnection` call, so that the resultant PeerConnections will be opened
   via STUNner as the TURN server. The generated ICE configs are always up to date with the most
   recent STUNner configuration, which makes sure that whenever you modify the STUNner Gateway API
   configuration (say, switch from `static` authentication to `ephemeral`), your clients will
   always receive an ICE config that reflects these changes (that is, the username/password pair
   will provide a time-windowed credential).
   
   For instance, the below will query the STUnner auth service, which is by default available at
   `stunner-auth.stunner-system:8088`, for a valid ICE config.

   ```console
   curl "http://stunner-auth.stunner-system:8088/ice?service=turn"
   {
     "iceServers": [
       {
         "username": "1681486023:"
         "credential": "v+MZOBeGWnk2690oJVL0qwF8YHQ=",
         "urls": [
           "turn:10.98.80.250:3478?transport=udp",
           "turn:10.105.169.152:3478?transport=tcp"
         ],
       }
     ],
     "iceTransportPolicy": "all"
   }
   ```

   Use the below to specify the lifetime of the generated credential to one hour (`ttl`, only makes sense when
   STUNner uses `ephemeral` authentication credentials) for a user named `my-user`, and you want
   the user to enter your cluster via the STUNner Gateway called `my-gateway` deployed into the
   `my-namespace` namespace.

   ```console
   curl "http://stunner-auth.stunner-system:8088/ice?service=turn?ttl=3600&username=my-user&namespace=my-namespace&gateway=my-gateway"
   ```
   
2. The clients *receive the ICE configuration over a secure channel.* This is outside the context
   of STUNner; our advice is to return the ICE configuration during the session setup process, say,
   along with the initial configuration returned for clients before starting the call.

3. WebRTC clients are *configured with the ICE configuration* obtained above. The below snippet
   shows how to initialize a WebRTC
   [`PeerConnection`](https://developer.mozilla.org/en-US/docs/Web/API/RTCPeerConnection/RTCPeerConnection)
   to use the above ICE server configuration in order to use STUNner as the default TURN service.

   ```javascript
   var iceConfig = <obtain ICE configuration sent by the application server>
   var pc = new RTCPeerConnection(iceConfig);
   ```

## Static authentication

In STUNner, `static` authentication is the simplest and least secure authentication mode, basically
corresponding to a traditional "log-in" username and password pair given to users. STUNner accepts
(and sometimes reports) the alias `plaintext` to mean the `static` authentication mode; the use of
`plaintext` is deprecated and will be removed in a later release.

When STUNner is configured to use `static` authentication only a single username/password pair is
used for *all* clients. This makes configuration easy; e.g., the ICE server configuration can be
hardcoded into the static Javascript code served to clients. At the same time, `static`
authentication is prone to leaking the credentials: once an attacker learns a username/password
pair they can use it without limits to reach STUNner (until the administrator rolls the
credentials, see below).

The first step of configuring STUNner for the `static` authentication mode is to create a
Kubernetes Secret to hold the username/password pair. The below will set the username to `my-user`
and the password to `my-password`. Note that if no `type` is set then STUNner defaults to `static`
authentication.

```console
kubectl -n stunner create secret generic stunner-auth-secret --from-literal=type=static \
    --from-literal=username=my-user --from-literal=password=my-password
```

Then, we create or update the current [GatewayConfig](REFERENCE.md) to refer STUNner to this secret
for setting the authentication credentials.

```yaml
apiVersion: stunner.l7mp.io/v1alpha1
kind: GatewayConfig
metadata:
  name: stunner-gatewayconfig
  namespace: stunner
spec:
  realm: stunner.l7mp.io
  authRef:
    name: stunner-auth-secret
    namespace: stunner
```

The main use of static authentication is for testing. The reason for this is that static
authentication credentials are easily discoverable: since the WebRTC Javascript API uses the TURN
credentials unencrypted, an attacker can easily extract the STUNner credentials from the
client-side Javascript code. In order to mitigate the risk, it is a good security practice to reset
the username/password pair every once in a while.  This can be done by simply updating the Secret
that holds the credentials.

```yaml
kubectl -n stunner edit secret stunner-auth-secret
```

Note that modifying STUNner's credentials goes *without* restarting the TURN server: new
connections will use the modified credentials but existing TURN connections should continue as
normal (the application server may need to be restarted to learn the new TURN credentials though).

## Ephemeral authentication

For production use, STUNner provides the `ephemeral` authentication mode that uses per-client
time-limited STUN/TURN authentication credentials.  Ephemeral credentials are dynamically generated
with a pre-configured lifetime and, once the lifetime expires, the credential cannot be used to
authenticate (or refresh) with STUNner any more. This authentication mode is more secure since
credentials are not shared between clients and come with a limited lifetime. Configuring
`ephemeral` authentication may be more complex though, since credentials must be dynamically
generated for each session and properly returned to clients. STUNner accepts (and sometimes
reports) the alias `longterm` to mean the `ephemeral` authentication mode; the use of `longterm` is
deprecated and will be removed in a later release. Note also that the alias `timewindowed` is also
accepted.

To implement this mode, STUNner adopts the [quasi-standard time-windowed TURN authentication
credential format](https://datatracker.ietf.org/doc/html/draft-uberti-behave-turn-rest-00). In this
format, the TURN username consists of a colon-delimited combination of the expiration timestamp and
the user-id parameter, where the user-id is some application-specific id that is opaque to STUNner
and the timestamp specifies the date of expiry of the credential as a UNIX timestamp. The TURN
password is computed from the a secret key shared with the TURN server and the returned username
value, by performing `base64(HMAC-SHA1(secret key, username))`. STUNner extends this scheme
somewhat for maximizing interoperability with WebRTC apps, in that it allows the user-id and the
timestamp to appear in any order in the TURN username and it accepts usernames with a plain
timestamp, without the colon and/or the user-id.

The advantage of this mechanism is that it is enough to know the shared secret for STUNner to be
able to check the validity of a credential. Note that the user-id is used only for the integrity
check but STUNner in no way checks whether it identifies a valid user-id in the system.

In order to switch from `static` mode to `ephemeral` authentication, it is enough to simply update
the Secret that holds the credentials. The below will set the shared secret `my-shared-secret` for
the ephemeral authentication mode.

```yaml
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: stunner-auth-secret
  namespace: stunner
type: Opaque
stringData:
  type: ephemeral
  secret: my-shared-secret
EOF
```

Obtaining an ICE config from the STUNner authentication service will now return an ephemeral TURN
credential that is valid only for a single day. 

```console
curl "http://stunner-auth.stunner-system:8088/ice?service=turn&username=user-id"
{
  "iceServers": [
    {
      "username": "1681490135:user-id"
      "credential": "WSfJZ8QIi8ebu1uWhxlclvyjEPY=",
      "urls": [
        "turn:10.105.169.152:3478?transport=tcp",
        "turn:10.98.80.250:3478?transport=udp"
      ],
    }
  ],
  "iceTransportPolicy": "all"
}
```
