package cu

// #include <cuda.h>
import "C"
import (
	"runtime"
	"sync"
	"unsafe"
)

var contextLock = new(sync.Mutex)
var pkgContext CUContext

// Context is
type Context interface {
	APIVersion() (int, error)

	SetCurrent() error
	MemAlloc(bytesize int64) (retVal DevicePtr, err error)
	MemAllocManaged(bytesize int64, flags MemAttachFlags) (DevicePtr, error)
	Memcpy(dst, src DevicePtr, byteCount int64)
	MemcpyHtoD(dst DevicePtr, src unsafe.Pointer, byteCount int64)
	MemcpyDtoH(dst unsafe.Pointer, src DevicePtr, byteCount int64)
	MemFree(mem DevicePtr)
	MemFreeHost(p unsafe.Pointer)
	LaunchKernel(function Function, gridDimX, gridDimY, gridDimZ int, blockDimX, blockDimY, blockDimZ int, sharedMemBytes int, stream Stream, kernelParams []unsafe.Pointer)
	Synchronize()
}

// Standalone is a standalone CUDA Context that is threadlocked.
type Standalone struct {
	CUContext
	work    chan (func() error)
	errChan chan error

	err error
}

func NewContext(d Device, flags ContextFlags) *Standalone {
	work := make(chan func() error)
	ctx := &Standalone{
		work:    work,
		errChan: make(chan error),
	}

	go ctx.run()

	var cuctx CUContext
	f := func() (err error) {
		var cctx C.CUcontext
		if err := result(C.cuCtxCreate(&cctx, C.uint(flags), C.CUdevice(d))); err != nil {
			return err
		}
		cuctx = CUContext(uintptr(unsafe.Pointer(cctx)))

		return nil
	}
	if err := ctx.Do(f); err != nil {
		panic(err)
	}

	// set it to package context
	contextLock.Lock()
	pkgContext = cuctx
	contextLock.Unlock()

	ctx.CUContext = cuctx

	runtime.SetFinalizer(ctx, finalizeStandalone)
	return ctx
}

func (ctx *Standalone) Do(fn func() error) error {
	ctx.work <- fn
	return <-ctx.errChan
}

func (ctx *Standalone) Err() error { return ctx.err }

func (ctx *Standalone) SetCurrent() error {
	f := func() (err error) {
		if ctx.CUContext != pkgContext {
			return SetCurrent(ctx.CUContext)
		}
		return
	}
	return ctx.Do(f)
}

func (ctx *Standalone) run() {
	runtime.LockOSThread()
	for w := range ctx.work {
		ctx.errChan <- w()
	}
	runtime.UnlockOSThread()
}

func finalizeStandalone(ctx *Standalone) {
	f := func() error {
		return result(C.cuCtxDestroy(C.CUcontext(unsafe.Pointer(&ctx.CUContext))))
	}
	if err := ctx.Do(f); err != nil {
		panic(err)
	}
	close(ctx.errChan)
	close(ctx.work)
}
