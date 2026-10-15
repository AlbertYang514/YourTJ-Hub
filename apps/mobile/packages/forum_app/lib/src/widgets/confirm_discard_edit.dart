import 'package:ui_kit/ui_kit.dart';
import 'package:flutter/material.dart';
import '../../l10n/app_localizations.dart';

Future<bool> confirmDiscardEdit(BuildContext context) async {
  final l = AppLocalizations.of(context);
  return await showDialog<bool>(
        context: context,
        animationStyle: GfMotion.dialogStyle(context),
        builder: (context) => AlertDialog(
          title: GfText(l.settingsUnsavedTitle),
          content: GfText(l.settingsUnsavedBody),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: GfText(l.settingsKeepEditing),
            ),
            TextButton(
              onPressed: () => Navigator.pop(context, true),
              child: GfText(l.settingsDiscardChanges),
            ),
          ],
        ),
      ) ??
      false;
}
