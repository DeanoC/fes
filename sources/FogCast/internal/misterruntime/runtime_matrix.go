package misterruntime

import (
	"context"
	"errors"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/protocol"
)

func (r *Runtime) lockComputer(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if r.computerMu.TryLock() {
			if err := ctx.Err(); err != nil {
				r.computerMu.Unlock()
				return err
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

// SetKeyboard retains the sink's exact operation witness until its source is
// confirmed neutral. The validator runs before the serialized post commits.
func (r *Runtime) SetKeyboard(ctx context.Context, post localhid.MatrixPost, matrix uint64) error {
	if !post.Witness.Valid() || post.Validate == nil {
		return errors.Join(localhid.ErrMatrixNotDispatched, errInvalidRuntimeRequest)
	}
	if err := r.lockComputer(ctx); err != nil {
		return errors.Join(localhid.ErrMatrixNotDispatched, err)
	}
	defer r.computerMu.Unlock()
	w := post.Witness
	return r.setKeyboardLocked(ctx, w.RuntimeInstance, w.PackageID, w.Generation, matrix, &post, nil)
}

// CheckMatrixAdmission proves that the adapter has no unresolved matrix
// operation before a source is admitted. validate checks the caller's fresh
// runtime/core observation and held lifecycle guard; it must not reenter the
// adapter or perform a physical write.
func (r *Runtime) CheckMatrixAdmission(ctx context.Context, instance, packageID string, generation uint64, validate func(context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if validate == nil || !protocol2Instance.MatchString(instance) || instance == "00000000000000000000000000000000" || generation == 0 || (packageID != "" && protocol.ValidateDigest(packageID) != nil) {
		return errInvalidRuntimeRequest
	}
	if err := r.lockComputer(ctx); err != nil {
		return err
	}
	defer r.computerMu.Unlock()
	if r.keyboardUncertain != nil || r.keyboardMediaUncertain != nil {
		return errors.New("keyboard state is unconfirmed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validate(ctx); err != nil {
		return err
	}
	return ctx.Err()
}

// MatrixMediaUncertainty returns a snapshot for a subsequent fresh retirement
// observation. The caller must not treat absence as proof of core retirement.
func (r *Runtime) MatrixMediaUncertainty(ctx context.Context) (localhid.MatrixMediaWitness, bool, error) {
	if err := r.lockComputer(ctx); err != nil {
		return localhid.MatrixMediaWitness{}, false, err
	}
	defer r.computerMu.Unlock()
	if r.keyboardMediaUncertain == nil {
		return localhid.MatrixMediaWitness{}, false, nil
	}
	return *r.keyboardMediaUncertain, true, nil
}

// NeutralMatrixMediaUncertainty posts only the all-released matrix against
// the exact original authority. Both matching uncertainties clear together
// only when the post and lifecycle validation are confirmed.
func (r *Runtime) NeutralMatrixMediaUncertainty(ctx context.Context, neutral localhid.MatrixMediaNeutral) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if neutral.Validate == nil {
		return errInvalidRuntimeRequest
	}
	if err := r.lockComputer(ctx); err != nil {
		return err
	}
	defer r.computerMu.Unlock()
	if r.keyboardMediaUncertain == nil || *r.keyboardMediaUncertain != neutral.MediaOf {
		return errors.New("media neutral does not match the uncertain operation")
	}
	if (r.keyboardUncertain == nil) != (neutral.SourceOf == nil) || (r.keyboardUncertain != nil && *r.keyboardUncertain != *neutral.SourceOf) {
		return errors.New("source neutral does not match the uncertain operation")
	}
	if r.keyboardUncertain != nil && (r.keyboardUncertain.RuntimeInstance != neutral.MediaOf.RuntimeInstance || r.keyboardUncertain.PackageID != neutral.MediaOf.PackageID || r.keyboardUncertain.Generation != neutral.MediaOf.Generation) {
		return errors.New("uncertain keyboard bindings differ")
	}
	control, ok := r.control.(protocol2KeyboardControl)
	if !ok {
		return unsupportedOperationError()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := neutral.Validate(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	response, err := control.SetKeyboard(ctx, neutral.MediaOf.RuntimeInstance, neutral.MediaOf.PackageID, neutral.MediaOf.Generation, 0xffffffffff)
	if err != nil {
		return err
	}
	if !response.OK {
		return mapProtocol2Error(response.Error)
	}
	if response.RuntimeInstance == nil || *response.RuntimeInstance != neutral.MediaOf.RuntimeInstance || response.Generation == nil || *response.Generation != neutral.MediaOf.Generation || (response.ActivePackage == nil && neutral.MediaOf.PackageID != "") || (response.ActivePackage != nil && response.ActivePackage.PackageID != neutral.MediaOf.PackageID) {
		return errInvalidRuntimeResponse
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := neutral.Validate(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.keyboardMediaUncertain = nil
	r.keyboardUncertain = nil
	r.keyboardMatrix = 0xffffffffff
	r.keyboardInstance, r.keyboardPackageID, r.keyboardGeneration = neutral.MediaOf.RuntimeInstance, neutral.MediaOf.PackageID, neutral.MediaOf.Generation
	return nil
}

// RetireMatrixMediaUncertainty clears only the exact failed media restoration
// after the lifecycle proves that its runtime/core binding has retired.
func (r *Runtime) RetireMatrixMediaUncertainty(ctx context.Context, proof localhid.MatrixMediaRetirement) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var zero [16]byte
	old := proof.RetireOf
	if old.Operation == zero || !protocol2Instance.MatchString(old.RuntimeInstance) || old.RuntimeInstance == "00000000000000000000000000000000" || old.Generation == 0 || (old.PackageID != "" && protocol.ValidateDigest(old.PackageID) != nil) || proof.Validate == nil || !protocol2Instance.MatchString(proof.ObservedRuntimeInstance) || proof.ObservedRuntimeInstance == "00000000000000000000000000000000" {
		return errInvalidRuntimeRequest
	}
	if proof.Idle {
		if proof.ObservedGeneration != 0 || proof.ObservedPackageID != "" {
			return errInvalidRuntimeRequest
		}
	} else if proof.ObservedGeneration == 0 || (proof.ObservedPackageID != "" && protocol.ValidateDigest(proof.ObservedPackageID) != nil) || (proof.ObservedRuntimeInstance == old.RuntimeInstance && proof.ObservedPackageID == old.PackageID && proof.ObservedGeneration == old.Generation) {
		return errInvalidRuntimeRequest
	}
	if err := r.lockComputer(ctx); err != nil {
		return err
	}
	defer r.computerMu.Unlock()
	if r.keyboardMediaUncertain == nil || *r.keyboardMediaUncertain != old {
		return errors.New("media retirement does not match the uncertain operation")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := proof.Validate(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.keyboardMediaUncertain = nil
	return nil
}

// RetireMatrixUncertainty clears only the exact failed operation after the
// input lifecycle proves that its old runtime/core binding has retired. It
// performs no physical keyboard write.
func (r *Runtime) RetireMatrixUncertainty(ctx context.Context, proof localhid.MatrixRetirement) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !proof.RetireOf.Valid() || proof.Validate == nil || !protocol2Instance.MatchString(proof.ObservedRuntimeInstance) || proof.ObservedRuntimeInstance == "00000000000000000000000000000000" {
		return errInvalidRuntimeRequest
	}
	if proof.Idle {
		if proof.ObservedGeneration != 0 || proof.ObservedPackageID != "" {
			return errInvalidRuntimeRequest
		}
	} else if proof.ObservedGeneration == 0 || (proof.ObservedPackageID != "" && protocol.ValidateDigest(proof.ObservedPackageID) != nil) || (proof.ObservedRuntimeInstance == proof.RetireOf.RuntimeInstance && proof.ObservedPackageID == proof.RetireOf.PackageID && proof.ObservedGeneration == proof.RetireOf.Generation) {
		return errInvalidRuntimeRequest
	}
	if err := r.lockComputer(ctx); err != nil {
		return err
	}
	defer r.computerMu.Unlock()
	if r.keyboardUncertain == nil || *r.keyboardUncertain != proof.RetireOf {
		return errors.New("keyboard retirement does not match the uncertain operation")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := proof.Validate(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.keyboardUncertain = nil
	return nil
}

func (r *Runtime) setKeyboardLocked(ctx context.Context, instance, packageID string, generation, matrix uint64, post *localhid.MatrixPost, media *localhid.MatrixMediaWitness) error {
	control, ok := r.control.(protocol2KeyboardControl)
	if !ok {
		return errors.Join(localhid.ErrMatrixNotDispatched, unsupportedOperationError())
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if r.keyboardMediaUncertain != nil || (r.keyboardUncertain != nil && (post == nil || post.NeutralOf == nil || *post.NeutralOf != *r.keyboardUncertain || post.Witness.Sink != r.keyboardUncertain.Sink || post.Witness.Source != r.keyboardUncertain.Source || post.Witness.RuntimeInstance != r.keyboardUncertain.RuntimeInstance || post.Witness.PackageID != r.keyboardUncertain.PackageID || post.Witness.Generation != r.keyboardUncertain.Generation)) {
		return errors.Join(localhid.ErrMatrixNotDispatched, errors.New("keyboard state is unconfirmed"))
	}
	if r.keyboardUncertain == nil && post != nil && post.NeutralOf != nil {
		return errors.Join(localhid.ErrMatrixNotDispatched, errors.New("keyboard neutral has no matching uncertain operation"))
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(localhid.ErrMatrixNotDispatched, err)
	}
	if post != nil {
		if err := post.Validate(ctx); err != nil {
			return errors.Join(localhid.ErrMatrixNotDispatched, err)
		}
		if err := ctx.Err(); err != nil {
			return errors.Join(localhid.ErrMatrixNotDispatched, err)
		}
	} else if media == nil || media.Operation == ([16]byte{}) || media.RuntimeInstance != instance || media.PackageID != packageID || media.Generation != generation {
		return errors.Join(localhid.ErrMatrixNotDispatched, errInvalidRuntimeRequest)
	}
	markUncertain := func() {
		if post == nil {
			copy := *media
			r.keyboardMediaUncertain = &copy
			return
		}
		if post != nil && r.keyboardUncertain == nil {
			witness := post.Witness
			r.keyboardUncertain = &witness
		}
	}
	response, err := control.SetKeyboard(ctx, instance, packageID, generation, matrix)
	if err != nil {
		markUncertain()
		if errors.Is(err, localhid.ErrMatrixNotDispatched) {
			return errors.New(err.Error())
		}
		return err
	}
	if !response.OK {
		markUncertain()
		return mapProtocol2Error(response.Error)
	}
	if response.RuntimeInstance == nil || *response.RuntimeInstance != instance || response.Generation == nil || *response.Generation != generation || (response.ActivePackage == nil && packageID != "") || (response.ActivePackage != nil && response.ActivePackage.PackageID != packageID) {
		markUncertain()
		return errInvalidRuntimeResponse
	}
	if err := ctx.Err(); err != nil {
		markUncertain()
		return err
	}
	if post != nil {
		if err := post.Validate(ctx); err != nil {
			markUncertain()
			if errors.Is(err, localhid.ErrMatrixNotDispatched) {
				return errors.New(err.Error())
			}
			return err
		}
		if err := ctx.Err(); err != nil {
			markUncertain()
			return err
		}
		if post.NeutralOf != nil {
			r.keyboardUncertain = nil
		}
	}
	r.keyboardMatrix = matrix
	r.keyboardInstance, r.keyboardPackageID, r.keyboardGeneration = instance, packageID, generation
	return nil
}
