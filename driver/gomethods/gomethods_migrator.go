package gomethods

import (
	"bufio"
	"fmt"
	"github.com/dimag-jfrog/migrate/driver"
	"github.com/dimag-jfrog/migrate/file"
	"os"
	"path"
	"strings"
)

type MissingMethodError string

func (e MissingMethodError) Error() string { return "Non existing migrate method: " + string(e) }

type WrongMethodSignatureError string

func (e WrongMethodSignatureError) Error() string {
	return fmt.Sprintf("Method %s has wrong signature", e)
}

type MethodInvocationFailedError struct {
	MethodName string
	Err        error
}

func (e *MethodInvocationFailedError) Error() string {
	return fmt.Sprintf("Method %s returned an error: %v", e.MethodName, e.Error)
}

type MigrationMethodInvoker interface {
	IsValid(methodName string) bool
	Invoke(methodName string) error
}

type GoMethodsDriver interface {
	driver.Driver

	MigrationMethodInvoker
	MethodsReceiver() interface{}
	SetMethodsReceiver(r interface{}) error
}

type Migrator struct {
	RollbackOnFailure bool
	MethodInvoker     MigrationMethodInvoker
}

func (m *Migrator) Migrate(f file.File, pipe chan interface{}) error {
	methods, err := m.getMigrationMethods(f)
	if err != nil {
		pipe <- err
		return err
	}

	for i, methodName := range methods {
		pipe <- methodName
		err := m.MethodInvoker.Invoke(methodName)
		if err != nil {
			pipe <- err
			if !m.RollbackOnFailure {
				return err
			}

			// on failure, try to rollback methods in this migration
			for j := i - 1; j >= 0; j-- {
				rollbackToMethodName := getRollbackToMethod(methods[j])
				if rollbackToMethodName == "" ||
					!m.MethodInvoker.IsValid(rollbackToMethodName) {
					continue
				}

				pipe <- rollbackToMethodName
				err = m.MethodInvoker.Invoke(rollbackToMethodName)
				if err != nil {
					pipe <- err
					break
				}
			}
			return err
		}
	}

	return nil
}

func reverseInPlace(a []string) {
	for i := 0; i < len(a)/2; i++ {
		j := len(a) - i - 1
		a[i], a[j] = a[j], a[i]
	}
}

func getRollbackToMethod(methodName string) string {
	if strings.HasSuffix(methodName, "_up") {
		return strings.TrimSuffix(methodName, "_up") + "_down"
	} else if strings.HasSuffix(methodName, "_down") {
		return strings.TrimSuffix(methodName, "_down") + "_up"
	} else {
		return ""
	}
}

func getFileLines(file file.File) ([]string, error) {
	if len(file.Content) == 0 {
		lines := make([]string, 0)
		file, err := os.Open(path.Join(file.Path, file.FileName))
		if err != nil {
			return nil, err
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)

		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		return lines, nil
	} else {
		s := string(file.Content)
		return strings.Split(s, "\n"), nil
	}
}

func (m *Migrator) getMigrationMethods(f file.File) (methods []string, err error) {
	var lines []string

	lines, err = getFileLines(f)
	if err != nil {
		return nil, err
	}

	for _, line := range lines {
		line := strings.TrimSpace(line)

		if line == "" || strings.HasPrefix(line, "--") {
			// an empty line or a comment, ignore
			continue
		}

		methodName := line
		if !m.MethodInvoker.IsValid(methodName) {
			return nil, MissingMethodError(methodName)
		}

		methods = append(methods, methodName)
	}

	return methods, nil

}
