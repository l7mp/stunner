# STUNner documentation

## Architecture

TODO

## Concepts

TODO

## User guides

<!-- * The [installation and configuration guide](/doc/INSTALL.md) helps getting started with STUnner -->
<!--   and describes the most important configuration knobs. -->
<!-- * The [security guide](/doc/SECURITY.md) discusses the best-practices to minmize the security risks -->
<!-- associated with a misconfigured STUNner gateway. -->
<!-- * The [authentication guide](/doc/AUTH.md) describes the different user authentication modes -->
<!-- supported by STUNner. -->

TODO

## Tutorials

STUNner comes with several tutorials that show how to use it to deploy different WebRTC
applications into Kubernetes.

* [Opening a UDP tunnel via STUNner](examples/simple-tunnel): This introductory tutorial shows how
  to tunnel an external connection via STUNner to a UDP service deployed into Kubernetes. The demo
  can be used to quickly check a STUNner installation.
* [Headless deployment: Direct one to one video call via STUNner](examples/direct-one2one-call):
  This tutorial showcases the [headless deployment model](#description), that is, when WebRTC
  clients connect to each other directly via STUNner using it as a TURN server, but without the
  mediation of a WebRTC media server.
* [Media-plane mode: One to one video call with Kurento via
  STUNner](examples/kurento-one2one-call): This tutorial extends the previous demo to showcase the
  [media-plane deployment model](#description), that is, when WebRTC clients connect to each other
  via a media server deployed into Kubernetes. This time, the media server is provided by
  [Kurento](https://www.kurento.org), but you can easily substitute your favorite media server
  instead. STUNner will ingest WebRTC media into the cluster and route it to Kurento, and all this
  happens *without* modifying the media server code in any way, just by adding 5-10 lines of
  straightforward JavaScript to configure clients to use STUNner as the TURN server.
* [Media-plane mode: Magic mirror via STUNner](examples/kurento-magic-mirror/README.md): This
  tutorial has been adopted from the [Kurento](https://www.kurento.org) [magic
  mirror](https://doc-kurento.readthedocs.io/en/stable/tutorials/node/tutorial-magicmirror.html)
  demo. The demo shows a basic WebRTC loopback server with some media processing added: the
  application uses computer vision and augmented reality techniques to add a funny hat on top of
  faces. The computer vision functionality is again provided by the [Kurento media
  server](https://www.kurento.org), being exposed to the clients via a STUNner gateway.
* [Media-plane mode: Cloud-gaming with STUNner](examples/cloudretro/README.md): If this was still
  not enough from the fun, this tutorial lets you play Super Mario or Street Fighter in your
  browser, courtesy of the amazing [CloudRetro](https://cloudretro.io) cloud-gaming project and of
  course STUNner. The tutorial shows how to deploy CloudRetro into Kubernetes, expose the media
  port via STUnner, and have endless retro-gaming fun!

## Manuals

* The [`stunnerd` manual](/cmd/stunnerd/README.md) describes the installation and configuration
  of `stunnerd`, the daemon that implements the STUNner gateway service.
* The [`turncat` manual](/cmd/turncat/README.md) describes the `turncat` utility, a simple
  STUN/TURN client to tunnel a local connection through a TURN server to an arbitrary remote
  address/port.
* The [`stunnerctl` manual](/cmd/stunnerctl/README.md) describes STUNner's CLI utility, which
  simplifies dealing with STUNner's configuration.

## Help

STUNner development is coordinated in Discord, send [us](/AUTHORS) an email to ask an invitation.

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](/AUTHORS).

MIT License - see [LICENSE](/LICENSE) for full text.
