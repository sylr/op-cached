//go:build darwin

package opcache

import (
	"errors"
	"fmt"

	"github.com/keybase/go-keychain"
)

// Service is the keychain service name every entry is filed under. Purge finds
// entries by querying it, so no separate index file is needed.
const Service = "op-cached"

// Keychain stores entries in a macOS keychain.
//
// Entries are created by this binary rather than by /usr/bin/security, so the
// keychain ACL trusts this binary alone. Another program cannot read them back
// by shelling out to `security`; it gets an authorisation prompt instead.
type Keychain struct{}

func (Keychain) item(key string) keychain.Item {
	item := keychain.NewItem()
	item.SetSecClass(keychain.SecClassGenericPassword)
	item.SetService(Service)
	item.SetAccount(key)
	return item
}

func (k Keychain) Get(key string) ([]byte, error) {
	value, err := keychain.GetGenericPassword(Service, key, "", "")
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, ErrNotFound
	}
	return value, nil
}

func (k Keychain) Put(key string, value []byte) error {
	item := k.item(key)
	item.SetLabel(fmt.Sprintf("%s (%s)", Service, key))
	item.SetData(value)
	// Entries are useless on another machine and must not leave this one.
	item.SetSynchronizable(keychain.SynchronizableNo)
	item.SetAccessible(keychain.AccessibleWhenUnlocked)

	err := keychain.AddItem(item)
	if errors.Is(err, keychain.ErrorDuplicateItem) {
		update := keychain.NewItem()
		update.SetData(value)
		return keychain.UpdateItem(k.item(key), update)
	}
	return err
}

func (k Keychain) Delete(key string) error { return keychain.DeleteItem(k.item(key)) }

func (k Keychain) Keys() ([]string, error) { return keychain.GetGenericPasswordAccounts(Service) }
