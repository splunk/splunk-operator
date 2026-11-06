/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package majorupgradeadapter

import (
	"context"
	"fmt"
	"slices"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
)

type cutoverSequence struct {
	schema, name string
	value        int64
	called       bool
}

func readCutoverSequences(ctx context.Context, conn pginfra.Conn) ([]cutoverSequence, error) {
	rows, err := conn.Query(ctx, cutoverSequenceInventorySQL)
	if err != nil {
		return nil, err
	}
	var sequences []cutoverSequence
	for rows.Next() {
		var seq cutoverSequence
		if err := rows.Scan(&seq.schema, &seq.name); err != nil {
			rows.Close()
			return nil, err
		}
		sequences = append(sequences, seq)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range sequences {
		seq := &sequences[i]
		rows, err := conn.Query(ctx, "SELECT last_value,is_called FROM "+pginfra.QuoteIdentifier(seq.schema)+"."+pginfra.QuoteIdentifier(seq.name))
		if err != nil {
			return nil, err
		}
		if !rows.Next() {
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("sequence state is absent")
		}
		err = rows.Scan(&seq.value, &seq.called)
		if err == nil && rows.Next() {
			err = fmt.Errorf("sequence state has multiple rows")
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return sequences, nil
}

func (s *postgresSwitchoverSQL) SynchronizeSequences(ctx context.Context, source, target *cnpgv1.Cluster, databases []declaredDatabase, id cutoverIdentity) error {
	// Only called while both environments are fenced and before canonical commit.
	for _, db := range databases {
		sequences, err := s.readEnvironmentSequences(ctx, source, db.name, id)
		if err != nil {
			return err
		}
		if err := s.withConn(ctx, target, "", db.name, id, func(conn pginfra.Conn) error { return syncCutoverSequences(ctx, conn, sequences) }); err != nil {
			return err
		}
	}
	return nil
}

func (s *postgresSwitchoverSQL) readEnvironmentSequences(ctx context.Context, cluster *cnpgv1.Cluster, database string, id cutoverIdentity) ([]cutoverSequence, error) {
	var sequences []cutoverSequence
	err := s.withConn(ctx, cluster, "", database, id, func(conn pginfra.Conn) error {
		var readErr error
		sequences, readErr = readCutoverSequences(ctx, conn)
		return readErr
	})
	if err != nil {
		return nil, err
	}
	return sequences, nil
}

// VerifySequenceParity reads both environments without changing their state.
func (s *postgresSwitchoverSQL) VerifySequenceParity(ctx context.Context, source, target *cnpgv1.Cluster, databases []declaredDatabase, id cutoverIdentity) (bool, error) {
	for _, db := range databases {
		blue, err := s.readEnvironmentSequences(ctx, source, db.name, id)
		if err != nil {
			return false, err
		}
		green, err := s.readEnvironmentSequences(ctx, target, db.name, id)
		if err != nil {
			return false, err
		}
		if !slices.Equal(blue, green) {
			return false, fmt.Errorf("%w: synchronized sequence state changed before endpoint commit", mvutypes.ErrUpgradeFlowFailed)
		}
	}
	return true, nil
}

func syncCutoverSequences(ctx context.Context, green pginfra.Conn, sequences []cutoverSequence) error {
	actual, err := readCutoverSequences(ctx, green)
	if err != nil {
		return err
	}
	if len(actual) != len(sequences) {
		return fmt.Errorf("%w: green sequence inventory changed", mvutypes.ErrUpgradeFlowFailed)
	}
	for i, seq := range sequences {
		if actual[i].schema != seq.schema || actual[i].name != seq.name {
			return fmt.Errorf("%w: green sequence identity changed", mvutypes.ErrUpgradeFlowFailed)
		}
		var value int64
		if err := queryOne(ctx, green, &value, `SELECT pg_catalog.setval($1::regclass,$2,$3)`, pginfra.QuoteIdentifier(seq.schema)+"."+pginfra.QuoteIdentifier(seq.name), seq.value, seq.called); err != nil {
			return err
		}
	}
	actual, err = readCutoverSequences(ctx, green)
	if err != nil {
		return err
	}
	if !slices.Equal(actual, sequences) {
		return fmt.Errorf("%w: green sequence state did not match blue after synchronization", mvutypes.ErrUpgradeFlowFailed)
	}
	return nil
}
