# Authentication

STUNner provides secure access to the WebRTC infrastructure deployed into Kubernetes. STUNner uses
the IETF TURN protocol to ingest media traffic into the Kubernetes cluster, which, [by
design](https://datatracker.ietf.org/doc/html/rfc5766#section-17), provides comprehensive
security. In particular, STUNner provides message integrity and, if configured with the TLS/TCP or
DTLS/UDP listeners, complete confidentiality. To complete the CIA triad, the this guide shows how
to add user authentication to STUNner.

## Table of Contents

## The long-term credential mechanism

STUNner relies on the STUN [long-term credential
mechanism](https://www.rfc-editor.org/rfc/rfc8489.html#page-26) to provide user authentication.

The long-term credential mechanism assumes that prior to the communication, the STUNner and the
WebRTC clients agree on a username and password to be used for authentication.  The credential is
considered long-term since it is assumed that it is provisioned for a user and remains in effect
until the user is no longer a subscriber of the system (`plaintext` authentication), or until the
predefined lifetime of the username/password pair passes and the credential expires. 

STUN secures the authentication process against replay attacks using a digest challenge.  In this
mechanism, the server sends the user a realm (used to guide the user or agent in selection of a
username and password) and a nonce.  The nonce provides replay protection.  The client also
includes a message-integrity attribute in the authentication message, which provides an HMAC over
the entire request, including the nonce.  The server validates the nonce and checks the message
integrity.  If they match, the request is authenticated, otherwise the server rejects the request.

## STUNner authentication workflow

The intended authentication workflow in STUNner is as follows.

1. *A username/password pair is generated.* This is outside the scope of STUNner; however, STUNner
   comes with a [small Node.js library](https://www.npmjs.com/package/@l7mp/stunner-auth-lib) that
   makes it simpler to generate STUNner credentials. For instance, the below will generate a
   username/password pair and a realm based on the current STUNner configuration.
   ```javascript
   const StunnerAuth = require('@l7mp/stunner-auth-lib');

   var credentials = StunnerAuth.getStunnerCredentials();
   ```
2. The clients and STUNner gateway exchange a username/password pair over a secure channel. The
   easiest way is to encode the username/password pair used for STUNner in the [ICE
   configuration](https://developer.mozilla.org/en-US/docs/Web/API/RTCIceServer) returned to
   clients. E.g., using the above [Node.js
   library](https://www.npmjs.com/package/@l7mp/stunner-auth-lib):
   ```javascript
   const StunnerAuth = require('@l7mp/stunner-auth-lib');
   ...
   var ICE_config = StunnerAuth.getIceConfig({
     address: '1.2.3.4',            // ovveride STUNNER_PUBLIC_ADDR
     port: 3478,                    // ovveride STUNNER_PUBLIC_PORT
     auth_type: 'plaintext',        // override STUNNER_AUTH_TYPE
     username: 'my-user',           // override STUNNER_USERNAME
     password: 'my-password',       // override STUNNER_PASSWORD
     ice_transport_policy: 'relay', // override STUNNER_ICE_TRANSPORT_POLICY
   });
   console.log(ICE_config);
   ```
   Output:
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
   
   For instance, in the [Magic mirror via STUNner](examples/kurento-magic-mirror/README.md) demo
   the ICE server configuration is generated and patched into the static Javascript code served to
   users on startup (this is suitable for `plaintext` authentication), while the [One to one video
   call with Kurento via STUNner](examples/kurento-one2one-call) demo generates the STUNner
   credentials dynamically, during user registration, and returns the full ICE server configuration
   to the clients in the "register response" message.
3. Use the STUNner credentials in the clients to initialize the WebRTC
   [`PeerConnection`](https://developer.mozilla.org/en-US/docs/Web/API/RTCPeerConnection/RTCPeerConnection).
   ```javascript
   var ICE_config = <Read ICE configuration send by the application server>
   var pc = new RTCPeerConnection(ICE_config);
   ```

## Plaintext authentication

In STUNner, `plaintext` authentication is the simplest and least secure authentication mode,
basically corresponding to a traditional "log-in" username and password pair given to users. Note
that only a single username/password pair is used for *all* clients. This makes configuration very
easy; e.g., the ICE server configuration can be written into the static Javascript code server to
clients. At the same time, `plaintext` authentication is the least secure mode: once an attacked
learns a `plaintext` STUNner credential they can use it without limits to reach STUNner, until the
administrator changes it (see below).

The below commands will configure STUNner to use `plaintext` authentication using the
username/password pair `my-user/my-password` and restart STUNner for the new configuration to take
effect.

```console
$ kubectl patch configmap/stunner-config --type merge \
  -p "{\"data\":{\"STUNNER_AUTH_TYPE\":\"plaintext\",\"STUNNER_USERNAME\":\"my-user\",\"STUNNER_PASSWORD\":\"my-password\"}}"
$ kubectl rollout restart deployment/stunner
```

The term `plaintext` may be deceptive: the password is never exchanged in plain text between the
client and STUNner. However, since the WebRTC Javascript API uses the TURN credentials unencrypted,
an attacker can easily extract the STUNner credentials from the client-side Javascript code.

In order to mitigate this risk, it is a good security practice to reset the username/password pair
every once in a while.  Suppose you want to set the STUN/TURN username to `my_user` and the
password to `my_pass`. To do this simply modify the STUNner `ConfigMap` and restart STUNner to
enforce the new access tokens:

```console
$ kubectl patch configmap/stunner-config -n default --type merge \
    -p "{\"data\":{\"STUNNER_USERNAME\":\"my_user\",\"STUNNER_PASSWORD\":\"my_pass\"}}"
$ kubectl rollout restart deployment/stunner
```

You can even set up a [cron
job](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs) to automate this. Note
that if the WebRTC application server uses [dynamic STUN/TURN credentials](#demo), then it may need
to be restarted as well to learn the new credentials.

## Longterm authentication

STUNner's longterm authentication mode provides clients time-limited access to STUNner.  STUNner
`longterm` credentials are dynamically generated with a pre-configured lifetime and, once the
lifetime expires, the credential cannot be used to authenticate (or refresh) with STUNner any
more. This authentication mode is more secure since credentials are not shared between clients and
come with a limited validity. Configuring `longterm` authentication is STUNner may be more complex,
however, since credentials must be dynamically generated for each session and properly returned to
clients.

STUNner adopts the [`longterm` authentication
mechanism](https://pkg.go.dev/github.com/pion/turn/v2#GenerateLongTermCredentials) from [Pion
TURN](https://pkg.go.dev/github.com/pion/turn/v2). In particular, the username is a UNIX timestamp
(in integer format) specifying the timestamp for the expiry of the credential, and the password is
base-64 encoded string obtained by SHA-hashing the timestamp with a predefined shared secret. The
advantage of this mechanism is that it is enough to know the shared secret for STUNner to be able
to check the validity of a credential.

The below commands will configure STUNner to use `longterm` authentication, using the shared secret
`my-secret`. By default, STUNner credentials are valid for one day.

```console
$ kubectl patch configmap/stunner-config --type merge \
  -p "{\"data\":{\"STUNNER_AUTH_TYPE\":\"longterm\",\"STUNNER_SHARED_SECRET\":\"my-secret\"}}"
$ kubectl rollout restart deployment/stunner
```

STUNner's [authentication helper library](https://www.npmjs.com/package/@l7mp/stunner-auth-lib)
will be able to correctly read the configuration from the `stunner-config` `ConfigMap` and use the
appropriate credential generators to create new username/password pairs.
```javascript
var cred = StunnerAuth.getStunnerCredentials({
    auth_type: 'longterm',   // override STUNNER_AUTH_TYPE
    secret: 'my-secret',     // override STUNNER_SHARED_SECRET
    duration: 24 * 60 * 60,  // lifetime the longterm credential is effective
});
```

## Help

STUNner development is coordinated in Discord, send [us](/AUTHORS) an email to ask an invitation.

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](/AUTHORS).

MIT License - see [LICENSE](/LICENSE) for full text.

## Acknowledgments

Initial code adopted from [pion/stun](https://github.com/pion/stun) and
[pion/turn](https://github.com/pion/turn).
