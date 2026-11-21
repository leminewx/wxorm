package session

import "reflect"

// 定义钩子函数的方法名
const (
	_HOOK_AFTER_SELECT  = "CallAfterSelect"
	_HOOK_AFTER_UPDATE  = "CallAfterUpdate"
	_HOOK_AFTER_DELETE  = "CallAfterDelete"
	_HOOK_AFTER_INSERT  = "CallAfterInsert"
	_HOOK_BEFORE_SELECT = "CallBeforeSelect"
	_HOOK_BEFORE_UPDATE = "CallBeforeUpdate"
	_HOOK_BEFORE_DELETE = "CallBeforeDelete"
	_HOOK_BEFORE_INSERT = "CallBeforeInsert"
)

type (
	// HookBeforeInsert 在插入数据之前调用
	HookBeforeInsert interface {
		CallBeforeInsert(sess *Session) error
	}

	// HookAfterInsert 在插入数据之后调用
	HookAfterInsert interface {
		CallAfterInsert(sess *Session) error
	}

	// HookBeforeUpdate 在更新数据之前调用
	HookBeforeUpdate interface {
		CallBeforeUpdate(sess *Session) error
	}

	// HookAfterUpdate 在更新数据之后调用
	HookAfterUpdate interface {
		CallAfterUpdate(sess *Session) error
	}

	// HookBeforeDelete 在删除数据之前调用
	HookBeforeDelete interface {
		CallBeforeDelete(sess *Session) error
	}

	// HookAfterDelete 在删除数据之后调用
	HookAfterDelete interface {
		CallAfterDelete(sess *Session) error
	}

	// HookAfterSelect 在查询数据之后调用
	HookAfterSelect interface {
		CallAfterSelect(sess *Session) error
	}

	// HookBeforeSelect 在查询数据之前调用
	HookBeforeSelect interface {
		CallBeforeSelect(sess *Session) error
	}
)

// 钩子接口对应的反射类型
//
// 热点路径（批量插入、多行查询）先用它判断目标类型是否实现钩子，
// 没有实现时跳过每行的接口装箱，避免无意义的分配。
var (
	hookBeforeInsertType = reflect.TypeOf((*HookBeforeInsert)(nil)).Elem()
	hookAfterInsertType  = reflect.TypeOf((*HookAfterInsert)(nil)).Elem()
	hookAfterSelectType  = reflect.TypeOf((*HookAfterSelect)(nil)).Elem()
)

// CallMethod 调用钩子函数
func (own *Session) CallMethod(method string, value any) error {
	switch method {
	// INSERT 前调用
	case _HOOK_BEFORE_INSERT:
		if value != nil {
			if hook, ok := value.(HookBeforeInsert); ok {
				return hook.CallBeforeInsert(own)
			}

			return nil
		}

		if hook, ok := own.model.(HookBeforeInsert); ok {
			return hook.CallBeforeInsert(own)
		}

	// INSERT 后调用
	case _HOOK_AFTER_INSERT:
		if value != nil {
			if hook, ok := value.(HookAfterInsert); ok {
				return hook.CallAfterInsert(own)
			}

			return nil
		}

		if hook, ok := own.model.(HookAfterInsert); ok {
			return hook.CallAfterInsert(own)
		}

	// UPDATE 前调用
	case _HOOK_BEFORE_UPDATE:
		if value != nil {
			if hook, ok := value.(HookBeforeUpdate); ok {
				return hook.CallBeforeUpdate(own)
			}

			return nil
		}

		if hook, ok := own.model.(HookBeforeUpdate); ok {
			return hook.CallBeforeUpdate(own)
		}

	// UPDATE 后调用
	case _HOOK_AFTER_UPDATE:
		if value != nil {
			if hook, ok := value.(HookAfterUpdate); ok {
				return hook.CallAfterUpdate(own)
			}

			return nil
		}

		if hook, ok := own.model.(HookAfterUpdate); ok {
			return hook.CallAfterUpdate(own)
		}

	// DELETE 前调用
	case _HOOK_BEFORE_DELETE:
		if value != nil {
			if hook, ok := value.(HookBeforeDelete); ok {
				return hook.CallBeforeDelete(own)
			}

			return nil
		}

		if hook, ok := own.model.(HookBeforeDelete); ok {
			return hook.CallBeforeDelete(own)
		}

	// DELETE 后调用
	case _HOOK_AFTER_DELETE:
		if value != nil {
			if hook, ok := value.(HookAfterDelete); ok {
				return hook.CallAfterDelete(own)
			}

			return nil
		}

		if hook, ok := own.model.(HookAfterDelete); ok {
			return hook.CallAfterDelete(own)
		}

	// SELECT 前调用
	case _HOOK_BEFORE_SELECT:
		if value != nil {
			if hook, ok := value.(HookBeforeSelect); ok {
				return hook.CallBeforeSelect(own)
			}

			return nil
		}

		if hook, ok := own.model.(HookBeforeSelect); ok {
			return hook.CallBeforeSelect(own)
		}

	// SELECT 后调用
	case _HOOK_AFTER_SELECT:
		if value != nil {
			if hook, ok := value.(HookAfterSelect); ok {
				return hook.CallAfterSelect(own)
			}

			return nil
		}

		if hook, ok := own.model.(HookAfterSelect); ok {
			return hook.CallAfterSelect(own)
		}
	}

	return nil
}
