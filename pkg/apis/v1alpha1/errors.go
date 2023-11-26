package v1alpha1

import stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"

var (
	ErrInvalidConf    = stnrv1.ErrInvalidConf
	ErrNoSuchListener = stnrv1.ErrNoSuchListener
	ErrNoSuchCluster  = stnrv1.ErrNoSuchCluster
)

type ErrRestarted = stnrv1.ErrRestarted
