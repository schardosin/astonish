(function_declaration name: (identifier) @definition.function)
(method_definition name: (property_identifier) @definition.method)
(class_declaration name: (identifier) @definition.class)
(lexical_declaration (variable_declarator name: (identifier) @definition.variable))
(variable_declaration (variable_declarator name: (identifier) @definition.variable))
(call_expression function: (identifier) @reference)
(call_expression function: (member_expression property: (property_identifier) @reference))
(member_expression property: (property_identifier) @reference)
(identifier) @reference
