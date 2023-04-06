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
until the user is no longer a subscriber of the system (STUNner's `plaintext` authentication mode),
or until the predefined lifetime of the username/password pair passes and the credential expires
(`longterm` authentication mode in STUNner).

STUNner secures the authentication process against replay attacks using a digest challenge.  In
this mechanism, the server sends the user a realm (used to guide the user or agent in selection of
a username and password) and a nonce.  The nonce provides replay protection.  The client also
includes a message-integrity attribute in the authentication message, which provides an HMAC over
the entire request, including the nonce.  The server validates the nonce and checks the message
integrity.  If they match, the request is authenticated, otherwise the server rejects the request.

## Authentication workflow

The intended authentication workflow in STUNner is as follows.

1. *A username/password pair is generated.* This is outside the scope of STUNner; however, STUNner
   comes with a [small Node.js library](https://www.npmjs.com/package/@l7mp/stunner-auth-lib) to
   simplify the generation of TURN credentials using STUNner's [running configuration](CONCEPTS.md). For
   instance, the below will automatically parse the running config and generate a username/password
   pair and a realm based on the current configuration.
```javascript
const StunnerAuth = require('@l7mp/stunner-auth-lib');
...
var credentials = StunnerAuth.getStunnerCredentials();
```
2. *The clients receive the username/password pair over a secure channel.* The
   easiest way is to encode the username/password pair used for STUNner is in the [ICE
   server configuration](https://developer.mozilla.org/en-US/docs/Web/API/RTCIceServer) returned to
   clients. E.g., using the above [Node.js
   library](https://www.npmjs.com/package/@l7mp/stunner-auth-lib):

```javascript
const StunnerAuth = require('@l7mp/stunner-auth-lib');
...
var ICE_config = StunnerAuth.getIceConfig({
  auth_type: 'plaintext',        // override the authentication type
  username: 'my-user',           // override username
  password: 'my-password',       // override password
  ice_transport_policy: 'relay', // override the ICE transport policy
});
console.log(ICE_config);
```
Note that the library is clever enough to parse out all settings from the running STUNner
configuration (e.g., the public IP address and port). All defaults  can be freely overridden
when calling `getIceConfig`. Below is a sample output:

```javascript
{
  iceServers: [
    {
      url: 'turn://1.2.3.4:3478?transport=udp',
      username: 'my-user',
      credential: 'my-password'
    }
  ],
  iceTransportPolicy: 'relay'
}
```

3. *WebRTC clients are configured with the STUNner authentication credentials.* The below snippet
   shows how to initialize a WebRTC
   [`PeerConnection`](https://developer.mozilla.org/en-US/docs/Web/API/RTCPeerConnection/RTCPeerConnection)
   to use the above ICE server configuration in order to use STUNner as the default TURN service.

```javascript
var ICE_config = <obtain ICE configuration sent by the application server>
var pc = new RTCPeerConnection(ICE_config);
```

## Plaintext authentication

In STUNner, `plaintext` authentication is the simplest and least secure authentication mode,
basically corresponding to a traditional "log-in" username and password pair given to users. Note
that only a single username/password pair is used for *all* clients. This makes configuration easy;
e.g., the ICE server configuration can be written into the static Javascript code served to
clients. At the same time, `plaintext` authentication is prone to leaking the credentials: once an
attacker learns a `plaintext` STUNner credential they can use it without limits to reach STUNner
(until the administrator rolls the credentials, see below).

You can select the authentication mode from the GatewayConfig resource of STUNner. For instance,
the below GatewayConfig will configure STUNner to use `plaintext` authentication using the
username/password pair `my-user/my-password` over the realm `my-realm.example.com`. Note that
`plaintext` authentication is the default in STUNner.

```yaml
apiVersion: stunner.l7mp.io/v1alpha1
kind: GatewayConfig
metadata:
  name: stunner-gatewayconfig
  namespace: stunner
spec:
  realm: my-realm.example.com
  authType: plaintext
  userName: "my-user"
  password: "my-pass"
```

The term `plaintext` may be deceptive: the password is never exchanged in plain text between the
client and STUNner over the Internet. However, since the WebRTC Javascript API uses the TURN
credentials unencrypted, an attacker can easily extract the STUNner credentials from the
client-side Javascript code. This does not pose a major security risk though: remember, possessing
a working TURN credential will allow an attacker to reach only the backend services explicitly
admitted by an appropriate UDPRoute. In other words, in a properly configured STUNner deployment
the attacker will be able to reach only the media servers, which is essentially the same level of
security as if you put the media servers to the Internet over an open public IP address. See
[here](SECURITY.md) for further tips on hardened STUNner deployments.

In order to mitigate the risk, it is a good security practice to reset the username/password pair
every once in a while.  Suppose you want to set the STUN/TURN username to `foo` and the password to
`bar`. To do this simply re-apply a modified GatewayConfig: the
[gateway-operator](CONCEPTS.md) will automatically reconcile the dataplane configuration to
enforce the new access tokens.

```yaml
apiVersion: stunner.l7mp.io/v1alpha1
kind: GatewayConfig
metadata:
  name: stunner-gatewayconfig
  namespace: stunner
spec:
  userName: "foo"
  password: "bar"
```

Note that modifying STUNner's credentials goes *without* restarting the TURN server: new
connections will use the modified credentials but existing TURN connections should continue as
normal (the application server may need to be restarted to learn the new TURN credentials though).

## Longterm authentication

Somewhat confusingly, STUNner overloads the name `longterm` to denote a STUN/TURN authentication
mode that provides clients time-limited access.  STUNner `longterm` credentials are dynamically
generated with a pre-configured lifetime and, once the lifetime expires, the credential cannot be
used to authenticate (or refresh) with STUNner any more. This authentication mode is more secure
since credentials are not shared between clients and come with a limited validity. Configuring
`longterm` authentication may be more complex though, since credentials must be dynamically
generated for each session and properly returned to clients.

To implement this mode, STUNner adopts the [quasi-standard time-windowed TURN authentication
credential format](https://datatracker.ietf.org/doc/html/draft-uberti-behave-turn-rest-00). In this
format, the TURN username consists of a colon-delimited combination of the expiration timestamp and
the user-id parameter, where the user-id is some application-specific id that is opaque to STUNner
and the timestamp specifies the date of expiry of the credential as a UNIX timestamp. Furthermore,
the TURN password is computed from the a secret key shared with the TURN server and the returned
username value, by performing `base64(HMAC-SHA1(secret key, username))`. STUNner extends this
scheme somewhat for maximizing interoperability with WebRTC apps, in that it allows the user-id and
the timestamp to appear in any order in the TURN username and it accepts usernames with a plain
timestamp, without the colon and/or the user-id.

The advantage of this mechanism is that it is enough to know the shared secret for STUNner to be
able to check the validity of a credential. Note that the user-id is used only for the integrity
check but STUNner in no way checks whether it identifies a valid user-id in the system.

The below commands will configure STUNner to use `longterm` authentication mode, using the shared
secret `my-secret`. By default, STUNner credentials are valid for one day.

```yaml
apiVersion: stunner.l7mp.io/v1alpha1
kind: GatewayConfig
metadata:
  name: stunner-gatewayconfig
  namespace: stunner
spec:
  realm: my-realm.example.com
  authType: longterm
  sharedSecret: "my-secret"
```

The below snippet shows how to use the [authentication helper lib](https://www.npmjs.com/package/@l7mp/stunner-auth-lib)
to generate the appropriate credentials to be returned to clients.
```javascript
var cred = StunnerAuth.getStunnerCredentials({
    auth_type: 'longterm',   // override authentication mode
    secret: 'my-secret',     // override the shared secret
    duration: 24 * 60 * 60,  // lifetime the longterm credential is effective
});
```

## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).

## License

Copyright 2021-2023 by its authors. Some rights reserved. See [AUTHORS](https://github.com/l7mp/stunner/blob/main/AUTHORS).

MIT License - see [LICENSE](https://github.com/l7mp/stunner/blob/main/LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).
