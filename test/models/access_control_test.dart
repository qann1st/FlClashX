import 'package:fl_clash/enum/enum.dart';
import 'package:fl_clash/models/models.dart';
import 'package:test/test.dart';

void main() {
  const accepting = AccessControlProps(
    enable: true,
    mode: AccessControlMode.acceptSelected,
    acceptList: ['a.app'],
    rejectList: ['r.app'],
  );
  const rejecting = AccessControlProps(
    enable: true,
    acceptList: ['a.app'],
    rejectList: ['r.app'],
  );

  test('no profile lists leave the picked apps alone', () {
    expect(
      accepting.withPackages(include: const [], exclude: const []),
      same(accepting),
    );
  });

  test('profile apps join the accepted ones', () {
    final props = accepting.withPackages(
      include: const ['p.app', 'a.app'],
      exclude: const ['x.app'],
    );

    expect(props.mode, AccessControlMode.acceptSelected);
    expect(props.acceptList, ['a.app', 'p.app']);
  });

  test('profile excluded apps join the rejected ones', () {
    final props = rejecting.withPackages(
      include: const [],
      exclude: const ['x.app'],
    );

    expect(props.mode, AccessControlMode.rejectSelected);
    expect(props.rejectList, ['r.app', 'x.app']);
  });

  test('an include list turns a rejecting list into an accepting one', () {
    final props = rejecting.withPackages(
      include: const ['p.app'],
      exclude: const ['x.app'],
    );

    expect(props.mode, AccessControlMode.acceptSelected);
    expect(props.acceptList, ['p.app']);
  });

  test('switched off access control takes only the profile lists', () {
    final props = accepting
        .copyWith(enable: false)
        .withPackages(include: const [], exclude: const ['x.app']);

    expect(props.enable, isTrue);
    expect(props.mode, AccessControlMode.rejectSelected);
    expect(props.rejectList, ['x.app']);
  });
}
