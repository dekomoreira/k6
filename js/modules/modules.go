/*
 *
 * k6 - a next-generation load testing tool
 * Copyright (C) 2020 Load Impact
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, either version 3 of the
 * License, or (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package modules

import (
	"fmt"
	"strings"

	"github.com/loadimpact/k6/js/internal/modules"
)

const extPrefix string = "k6/x/"

// GetModule returns the module registered with name.
func GetModule(name string) interface{} {
	return modules.GetModule(name)
}

// RegisterModule registers the given mod as an external JavaScript module,
// available for import from JS scripts with the "k6/x/<name>" import path.
func RegisterModule(name string, mod interface{}) {
	if !strings.HasPrefix(name, extPrefix) {
		name = fmt.Sprintf("%s%s", extPrefix, name)
	}

	modules.RegisterModule(name, mod)
}
