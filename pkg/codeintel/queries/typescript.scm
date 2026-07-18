(function_declaration name: (identifier) @definition.function)
(method_definition name: (property_identifier) @definition.method)
(class_declaration name: (type_identifier) @definition.class)
(interface_declaration name: (type_identifier) @definition.interface)
(type_alias_declaration name: (type_identifier) @definition.type)
(lexical_declaration (variable_declarator name: (identifier) @definition.variable))
(variable_declaration (variable_declarator name: (identifier) @definition.variable))
(call_expression function: (identifier) @reference)
(call_expression function: (member_expression property: (property_identifier) @reference))
(member_expression property: (property_identifier) @reference)
(type_identifier) @reference
(identifier) @reference
